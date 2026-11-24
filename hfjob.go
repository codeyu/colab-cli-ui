package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// A model download runs as a detached process on the VM, not inside the
// colab exec that starts it: a long exec can end with "Connection was lost"
// halfway. The app starts the job with one short exec, then reads its log
// and status with short, quiet ones until it ends.

const jobsDir = "/root/.colab-cli-ui/jobs"

// hfJobScript is the job itself: %s are Python literals (repo, files, dest,
// job dir) and %d the total size.
const hfJobScript = `import json, os, shutil, sys, threading, time, traceback
repo, files, dest, job, total = %s, %s, %s, %s, %d
def status(**kw):
    tmp = os.path.join(job, "status.json.tmp")
    with open(tmp, "w") as fh:
        json.dump(kw, fh)
    os.replace(tmp, os.path.join(job, "status.json"))
status(state="running")
try:
    tok = "/` + hfTokenUpload + `"
    if os.path.exists(tok):
        from huggingface_hub import constants as hc
        os.makedirs(os.path.dirname(hc.HF_TOKEN_PATH), exist_ok=True)
        shutil.move(tok, hc.HF_TOKEN_PATH)
        os.chmod(hc.HF_TOKEN_PATH, 0o600)
    from huggingface_hub import hf_hub_download
    from huggingface_hub.utils import disable_progress_bars
    disable_progress_bars()
    os.makedirs(dest, exist_ok=True)
    def size():
        n = 0
        for r, _, fs in os.walk(dest):
            for f in fs:
                try:
                    n += os.path.getsize(os.path.join(r, f))
                except OSError:
                    pass
        return n
    base, stop, t0 = size(), threading.Event(), time.time()
    def report():
        while not stop.wait(3):
            n = max(0, size() - base)
            s = n / max(1e-6, time.time() - t0)
            p = " %%.0f%%%%" %% (100 * n / total) if total else ""
            print("\r  %%.2f / %%.2f GB%%s · %%.1f MB/s" %% (n / 1e9, total / 1e9, p, s / 1e6), end="", flush=True)
    th = threading.Thread(target=report, daemon=True)
    th.start()
    try:
        for i, f in enumerate(files, 1):
            print("\n[%%d/%%d] %%s" %% (i, len(files), f), flush=True)
            hf_hub_download(repo, f, local_dir=dest)
    finally:
        stop.set()
        th.join()
    print("\n✓ %%d 个文件，用时 %%.0fs → %%s" %% (len(files), time.time() - t0, dest), flush=True)
    status(state="done")
except BaseException as e:
    traceback.print_exc()
    status(state="error", error="%%s: %%s" %% (type(e).__name__, e))
`

const jobLauncher = `import base64, json, os, subprocess, sys
job = %s
os.makedirs(job, exist_ok=True)
with open(os.path.join(job, "run.py"), "wb") as fh:
    fh.write(base64.b64decode(%s))
log = open(os.path.join(job, "log.txt"), "ab")
env = dict(os.environ, HF_HUB_DISABLE_PROGRESS_BARS="1", PYTHONUNBUFFERED="1", PYTHONIOENCODING="utf-8")
p = subprocess.Popen([sys.executable, os.path.join(job, "run.py")], stdout=log, stderr=subprocess.STDOUT,
                     stdin=subprocess.DEVNULL, start_new_session=True, env=env, cwd="/content")
with open(os.path.join(job, "pid"), "w") as fh:
    fh.write(str(p.pid))
print("__COLAB_UI_JOB__" + json.dumps({"pid": p.pid}))
`

const jobPoll = `import json, os, sys
job, off = %s, %d
try:
    with open(os.path.join(job, "log.txt"), "rb") as fh:
        fh.seek(off)
        data = fh.read(1 << 20)
except OSError:
    data = b""
try:
    with open(os.path.join(job, "status.json")) as fh:
        st = json.load(fh)
except Exception:
    st = {}
alive = False
try:
    with open("/proc/%%s/stat" %% open(os.path.join(job, "pid")).read().strip()) as fh:
        alive = fh.read().split(") ")[-1][:1] not in ("Z", "X")
except Exception:
    pass
sys.stdout.write(data.decode("utf-8", "replace"))
print("\n__COLAB_UI_POLL__" + json.dumps({"offset": off + len(data), "state": st.get("state", ""), "error": st.get("error", ""), "alive": alive}))
`

const jobKill = `import os, signal
try:
    os.killpg(int(open(os.path.join(%s, "pid")).read().strip()), signal.SIGTERM)
    print("killed")
except Exception as e:
    print("not running:", e)
`

type jobPollResult struct {
	Offset int64  `json:"offset"`
	State  string `json:"state"`
	Error  string `json:"error"`
	Alive  bool   `json:"alive"`
}

// splitPoll separates the log text a poll printed from its status line.
func splitPoll(out string) (string, *jobPollResult) {
	i := strings.LastIndex(out, "__COLAB_UI_POLL__")
	if i < 0 {
		return "", nil
	}
	var r jobPollResult
	line := out[i+len("__COLAB_UI_POLL__"):]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	if json.Unmarshal([]byte(strings.TrimSpace(line)), &r) != nil {
		return "", nil
	}
	text := strings.TrimSuffix(strings.TrimSuffix(out[:i], "\n"), "\r")
	return text, &r
}

// HFDownload downloads files of a model onto the session's VM with
// huggingface_hub, in a job that outlives a dropped connection, streaming its
// progress.
func (c *Colab) HFDownload(ctx context.Context, name string, opts HFDownloadOptions, out *mygo.Channel[Chunk]) error {
	if err := checkName(name); err != nil {
		return err
	}
	if !hfRepoID.MatchString(opts.Repo) {
		return fmt.Errorf("模型 ID 无效：%q", opts.Repo)
	}
	if len(opts.Files) == 0 {
		return errors.New("没有选择文件")
	}
	for _, f := range opts.Files {
		if !hfFileName.MatchString(f) || strings.HasPrefix(f, "/") || strings.Contains(f, "..") {
			return fmt.Errorf("文件名无效：%q", f)
		}
	}
	dest := path.Clean("/" + strings.ReplaceAll(opts.Dest, "\\", "/"))
	if dest == "/" {
		return errors.New("目标目录无效")
	}

	if tok, err := mygo.Secrets.Get(hfSecretKey); err == nil && len(tok) > 0 {
		if err := c.uploadSecret(ctx, name, tok); err != nil {
			return fmt.Errorf("上传 HuggingFace token 失败：%w", err)
		}
		_ = out.Send(Chunk{Stream: "info", Text: "已向 VM 提供 HuggingFace token\n"})
	}

	idb := make([]byte, 6)
	rand.Read(idb)
	job := jobsDir + "/" + hex.EncodeToString(idb)
	script := fmt.Sprintf(hfJobScript, pyLiteral(opts.Repo), pyLiteral(opts.Files), pyLiteral(dest), pyLiteral(job), opts.TotalBytes)
	launch := fmt.Sprintf(jobLauncher, pyLiteral(job), pyLiteral(base64.StdEncoding.EncodeToString([]byte(script))))
	res, err := c.run(ctx, internal(launch), "exec", "-s", name)
	if err != nil {
		return err
	}
	if !strings.Contains(res, "__COLAB_UI_JOB__") {
		return fmt.Errorf("没能在 VM 上启动下载：%s", tail(strings.TrimSpace(stripANSI(res)), 600))
	}
	_ = out.Send(Chunk{Stream: "info", Text: "下载已在 VM 后台开始\n"})

	var off int64
	failures := 0
	for polls := 0; ; polls++ {
		select {
		case <-ctx.Done():
			kctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			_, _ = c.runQuiet(kctx, internal(fmt.Sprintf(jobKill, pyLiteral(job))), "exec", "-s", name)
			cancel()
			return errors.New("已取消")
		case <-time.After(2 * time.Second):
		}
		res, err := c.runQuiet(ctx, internal(fmt.Sprintf(jobPoll, pyLiteral(job), off)), "exec", "-s", name)
		text, st := splitPoll(stripANSI(res))
		if err != nil || st == nil {
			if ctx.Err() != nil {
				continue // the select above handles it
			}
			if failures++; failures >= 5 {
				if err == nil {
					err = errors.New(tail(strings.TrimSpace(stripANSI(res)), 400))
				}
				return fmt.Errorf("读取下载进度失败（下载可能仍在 VM 上进行）：%w", err)
			}
			continue
		}
		failures = 0
		if text != "" {
			if out.Send(Chunk{Stream: "stdout", Text: text}) != nil {
				return nil // the page stopped listening; the job carries on
			}
		}
		off = st.Offset
		switch {
		case st.State == "done":
			return nil
		case st.State == "error":
			return errors.New("下载失败：" + st.Error)
		case !st.Alive && polls > 0:
			return errors.New("下载进程意外退出")
		}
	}
}
