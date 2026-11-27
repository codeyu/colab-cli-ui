package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// A model's API: Ollama serves a model on the VM with an OpenAI-compatible
// API on port 11434, which port forwarding brings to 127.0.0.1, for any
// client that speaks that API. Ollama installs in a minute, runs on the GPU
// when there is one, and pulls GGUF models from its library or straight from
// HuggingFace (hf.co/user/repo:QUANT).

const ollamaPort = 11434

// LLMDeployOptions say what to serve.
type LLMDeployOptions struct {
	// Model is an Ollama model ("qwen3:8b", "hf.co/user/repo:Q4_K_M"), or
	// the name to give the file at Path.
	Model string `json:"model"`
	// Path is a GGUF file on the VM (as on a mounted Drive) to serve
	// instead of pulling Model; empty to pull.
	Path string `json:"path"`
	// Context is the context length in tokens.
	Context int `json:"context"`
}

var ollamaModel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,200}$`)

// llmDeployScript installs and starts Ollama when needed, then pulls (or
// creates) the model and loads it. %s are Python literals: model, path, job;
// %d the context length.
const llmDeployScript = `import json, os, shutil, subprocess, sys, time, traceback, urllib.request
model, gguf, job, ctxlen = %s, %s, %s, %d
API = "http://127.0.0.1:11434"
STATE = "/root/.colab-cli-ui/ollama.json"
def status(**kw):
    tmp = os.path.join(job, "status.json.tmp")
    with open(tmp, "w") as fh:
        json.dump(kw, fh)
    os.replace(tmp, os.path.join(job, "status.json"))
def call(p, body=None, timeout=10, method=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(API + p, data=data, method=method, headers={"Content-Type": "application/json"})
    return urllib.request.urlopen(req, timeout=timeout)
def up():
    try:
        call("/api/version", timeout=2).read()
        return True
    except Exception:
        return False
def gb(n):
    return "%%.2f GB" %% (n / 1e9)
status(state="running")
try:
    if not shutil.which("ollama"):
        print("[1/4] 安装 Ollama（约 1 分钟）…", flush=True)
        if not shutil.which("zstd"):
            subprocess.run("apt-get -qq update && apt-get -qq install -y zstd", shell=True, stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT)
        r = subprocess.run("curl -fsSL https://ollama.com/install.sh | sh", shell=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        if r.returncode != 0 or not shutil.which("ollama"):
            print(r.stdout[-3000:])
            raise RuntimeError("Ollama 安装失败")
    else:
        print("[1/4] Ollama 已安装", flush=True)
    try:
        prev = json.load(open(STATE))
    except Exception:
        prev = {}
    if up() and prev.get("context") != ctxlen:
        print("      上下文长度改为 %%d，重启服务" %% ctxlen, flush=True)
        subprocess.run(["pkill", "-f", "ollama serve"])
        for _ in range(20):
            if not up():
                break
            time.sleep(0.5)
    if not up():
        print("[2/4] 启动 Ollama 服务…", flush=True)
        os.makedirs(os.path.dirname(STATE), exist_ok=True)
        env = dict(os.environ, OLLAMA_HOST="127.0.0.1:11434", OLLAMA_CONTEXT_LENGTH=str(ctxlen), OLLAMA_KEEP_ALIVE="-1")
        log = open("/root/.colab-cli-ui/ollama.log", "ab")
        subprocess.Popen(["ollama", "serve"], stdout=log, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, start_new_session=True, env=env, cwd="/root")
        for _ in range(120):
            if up():
                break
            time.sleep(0.5)
        else:
            raise RuntimeError("Ollama 服务没有启动，日志在 /root/.colab-cli-ui/ollama.log")
        with open(STATE, "w") as fh:
            json.dump({"context": ctxlen}, fh)
    else:
        print("[2/4] Ollama 服务已在运行", flush=True)
    t0 = time.time()
    if gguf:
        print("[3/4] 从 %%s 创建模型 %%s…" %% (gguf, model), flush=True)
        if not os.path.isfile(gguf):
            raise RuntimeError("找不到文件 %%s（Drive 需要先挂载）" %% gguf)
        mf = os.path.join(job, "Modelfile")
        with open(mf, "w") as fh:
            fh.write("FROM %%s\n" %% gguf)
        r = subprocess.run(["ollama", "create", model, "-f", mf], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        if r.returncode != 0:
            print(r.stdout[-3000:])
            raise RuntimeError("创建模型失败")
    else:
        print("[3/4] 下载模型 %%s…" %% model, flush=True)
        last, prev_status = 0, ""
        for line in call("/api/pull", {"model": model, "stream": True}, timeout=None):
            ev = json.loads(line)
            if ev.get("error"):
                raise RuntimeError(ev["error"])
            st = ev.get("status", "")
            if ev.get("total", 0) >= 1e6 and ev.get("completed") is not None:
                if time.time() - last >= 3 or ev["completed"] >= ev["total"]:
                    s = ev["completed"] / max(1e-6, time.time() - t0)
                    print("\r      %%s / %%s %%3.0f%%%% · %%.1f MB/s" %% (gb(ev["completed"]), gb(ev["total"]), 100 * ev["completed"] / ev["total"], s / 1e6), end="", flush=True)
                    last = time.time()
            elif st and st != prev_status and not ev.get("total"):
                print("\n      " + st, end="", flush=True)
            prev_status = st
        print(flush=True)
    print("[4/4] 加载到%%s…" %% ("显存" if shutil.which("nvidia-smi") else "内存"), flush=True)
    call("/api/generate", {"model": model, "prompt": "", "keep_alive": -1}, timeout=1800).read()
    for m in json.load(call("/api/ps")).get("models", []):
        if m.get("name") == model or m.get("model") == model:
            size, vram = m.get("size", 0), m.get("size_vram", 0)
            where = "全部在 GPU 上" if size and vram >= size else ("%%s 在 GPU 上，其余在 CPU 上（会变慢）" %% gb(vram) if vram else "在 CPU 上")
            print("      占用 %%s，%%s" %% (gb(size), where), flush=True)
    print("✓ %%s 已就绪，用时 %%.0fs" %% (model, time.time() - t0), flush=True)
    status(state="done")
except BaseException as e:
    traceback.print_exc()
    status(state="error", error="%%s: %%s" %% (type(e).__name__, e))
`

// LLMDeploy serves a model with Ollama on the session's VM, streaming the
// install, download and load. Port forwarding then brings the API here.
func (c *Colab) LLMDeploy(ctx context.Context, name string, opts LLMDeployOptions, out *mygo.Channel[Chunk]) error {
	if err := checkName(name); err != nil {
		return err
	}
	if !ollamaModel.MatchString(opts.Model) {
		return fmt.Errorf("模型名无效：%q", opts.Model)
	}
	gguf := ""
	if opts.Path != "" {
		gguf = path.Clean("/" + strings.ReplaceAll(opts.Path, "\\", "/"))
		if !strings.HasSuffix(strings.ToLower(gguf), ".gguf") {
			return errors.New("只支持 .gguf 文件")
		}
	}
	if opts.Context < 512 || opts.Context > 262144 {
		opts.Context = 8192
	}
	job := newJobDir()
	script := fmt.Sprintf(llmDeployScript, pyLiteral(opts.Model), pyLiteral(gguf), pyLiteral(job), opts.Context)
	return c.runJob(ctx, name, job, script, "部署", out)
}

// LLMModel is a model Ollama has on the VM.
type LLMModel struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Loaded is true when it is in memory; VRAM is how much of it is on
	// the GPU.
	Loaded bool  `json:"loaded"`
	VRAM   int64 `json:"vram"`
}

// LLMStatus is Ollama's state on a VM.
type LLMStatus struct {
	Installed bool       `json:"installed"`
	Running   bool       `json:"running"`
	Version   string     `json:"version"`
	Context   int        `json:"context"`
	Models    []LLMModel `json:"models"`
}

const llmStatusScript = `import json, shutil, urllib.request
r = {"installed": bool(shutil.which("ollama")), "running": False, "version": "", "context": 0, "models": []}
def get(p):
    return json.load(urllib.request.urlopen("http://127.0.0.1:11434" + p, timeout=3))
try:
    r["version"] = get("/api/version").get("version", "")
    r["running"] = True
    ps = {m.get("name"): m for m in get("/api/ps").get("models", [])}
    for m in get("/api/tags").get("models", []):
        p = ps.get(m.get("name"))
        r["models"].append({"name": m.get("name"), "size": m.get("size", 0), "loaded": p is not None, "vram": (p or {}).get("size_vram", 0)})
except Exception:
    pass
try:
    r["context"] = json.load(open("/root/.colab-cli-ui/ollama.json")).get("context", 0)
except Exception:
    pass
print("__COLAB_UI_LLM__" + json.dumps(r))
`

// GetLLMStatus reports whether Ollama runs on the session's VM and its models.
func (c *Colab) GetLLMStatus(ctx context.Context, name string) (LLMStatus, error) {
	if err := checkName(name); err != nil {
		return LLMStatus{}, err
	}
	out, err := c.runQuiet(ctx, internal(llmStatusScript), "exec", "-s", name)
	if err != nil {
		return LLMStatus{}, err
	}
	i := strings.LastIndex(out, "__COLAB_UI_LLM__")
	if i < 0 {
		return LLMStatus{}, fmt.Errorf("读取 Ollama 状态失败：%s", tail(strings.TrimSpace(stripANSI(out)), 400))
	}
	line := out[i+len("__COLAB_UI_LLM__"):]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	var st LLMStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &st); err != nil {
		return LLMStatus{}, err
	}
	if st.Models == nil {
		st.Models = []LLMModel{}
	}
	return st, nil
}

const llmStopScript = `import subprocess
subprocess.run(["pkill", "-f", "ollama serve"])
print("stopped")
`

const llmRemoveScript = `import json, urllib.request
req = urllib.request.Request("http://127.0.0.1:11434/api/delete", data=json.dumps({"model": %s}).encode(), method="DELETE", headers={"Content-Type": "application/json"})
urllib.request.urlopen(req, timeout=30).read()
print("removed")
`

// StopLLM stops the Ollama server on the VM, freeing the GPU.
func (c *Colab) StopLLM(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	_, err := c.runQuiet(ctx, internal(llmStopScript), "exec", "-s", name)
	return err
}

// RemoveLLMModel deletes a model from the VM's Ollama.
func (c *Colab) RemoveLLMModel(ctx context.Context, name, model string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if !ollamaModel.MatchString(model) {
		return fmt.Errorf("模型名无效：%q", model)
	}
	out, err := c.runQuiet(ctx, internal(fmt.Sprintf(llmRemoveScript, pyLiteral(model))), "exec", "-s", name)
	if err == nil && !strings.Contains(out, "removed") {
		err = fmt.Errorf("删除失败：%s", tail(strings.TrimSpace(stripANSI(out)), 400))
	}
	return err
}

// LLMChat sends a prompt to a model through a forwarded port, streaming
// the answer: the test that the API works end to end.
func (c *Colab) LLMChat(ctx context.Context, localPort int, model, prompt string, out *mygo.Channel[Chunk]) error {
	tunnels.Lock()
	ok := false
	for _, f := range tunnels.forwards {
		ok = ok || f.LocalPort == localPort
	}
	tunnels.Unlock()
	if !ok {
		return errors.New("这个端口没有在转发")
	}
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://127.0.0.1:%d/api/chat", localPort), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// No keep-alive: an idle pooled connection would show as open on the forward.
	resp, err := (&http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{DisableKeepAlives: true}}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("HTTP %d：%s", resp.StatusCode, e.Error)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	thinking := false
	for sc.Scan() {
		var ev struct {
			Message struct {
				Content  string `json:"content"`
				Thinking string `json:"thinking"`
			} `json:"message"`
			Done      bool   `json:"done"`
			Error     string `json:"error"`
			EvalCount int    `json:"eval_count"`
			EvalNs    int64  `json:"eval_duration"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Error != "" {
			return errors.New(ev.Error)
		}
		if t := ev.Message.Thinking; t != "" {
			thinking = true
			if out.Send(Chunk{Stream: "info", Text: t}) != nil {
				return nil
			}
		}
		if t := ev.Message.Content; t != "" {
			if thinking {
				thinking = false
				t = "\n\n" + t
			}
			if out.Send(Chunk{Stream: "stdout", Text: t}) != nil {
				return nil
			}
		}
		if ev.Done && ev.EvalNs > 0 {
			_ = out.Send(Chunk{Stream: "info", Text: fmt.Sprintf("\n\n%d tokens · %.1f tokens/s\n", ev.EvalCount, float64(ev.EvalCount)/(float64(ev.EvalNs)/1e9))})
		}
	}
	return sc.Err()
}
