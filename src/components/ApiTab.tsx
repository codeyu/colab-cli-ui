import { useEffect, useMemo, useRef, useState } from "react";
import { Bot, Copy, Power, RefreshCw, Rocket, Search, Send, Square, Trash2 } from "lucide-react";
import { Colab, errorMessage, isAbort, streamCall, type HFFile, type HFModel, type LLMStatus, type Session } from "../lib/api";
import { acceleratorInfo } from "../lib/accelerators";
import { forwardURL, useForwards } from "../lib/forwards";
import { bytes, cx } from "../lib/format";
import { cellsStore, toast, useStore } from "../lib/store";
import { runCell } from "./ConsoleTab";
import { Output } from "./Output";
import { ForwardRow, copyText, openForward } from "./PortsTab";
import { Badge, Button, Card, Segmented, Spinner, inputClass } from "./ui";

const OLLAMA_PORT = 11434;

/** Ollama library models, with parameters in billions for a size estimate. */
const PRESETS: { model: string; params: number }[] = [
  { model: "qwen3:1.7b", params: 1.7 },
  { model: "gemma3:4b", params: 4 },
  { model: "qwen3:8b", params: 8.2 },
  { model: "gemma3:12b", params: 12 },
  { model: "qwen3:14b", params: 14.8 },
  { model: "gpt-oss:20b", params: 21 },
  { model: "gemma3:27b", params: 27 },
  { model: "qwen3:32b", params: 32.8 },
];

const CONTEXTS = [4096, 8192, 16384, 32768, 65536];

type Source = "library" | "hf" | "file";

/** About how much memory a 4-bit model of this size takes, in GB. */
function estimateGB(params: number): number {
  return params * 0.6 + 1;
}

/** The quantization in a GGUF file name, as Ollama's hf.co tags name it. */
function quantOf(file: string): string | null {
  const m = file.match(/(?:^|[-_.])((?:UD-)?(?:I?Q\d+(?:_[A-Z0-9]+)*|BF16|F16|F32))\.gguf$/i);
  return m?.[1] ?? null;
}

function usableGGUF(f: HFFile): boolean {
  const n = f.name.toLowerCase();
  return n.endsWith(".gguf") && !n.includes("mmproj") && !/-\d{5}-of-\d{5}/.test(n);
}

export function ApiTab({ session }: { session: Session }) {
  const name = session.name;
  const info = acceleratorInfo(session.hardware);
  const vram = info?.vramGB ?? 0;
  const [status, setStatus] = useState<LLMStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [source, setSource] = useState<Source>("library");
  const [model, setModel] = useState(vram === 0 ? "qwen3:1.7b" : vram < 20 ? "qwen3:8b" : "qwen3:14b");
  const [path, setPath] = useState("/content/drive/MyDrive/");
  const [fileModel, setFileModel] = useState("");
  const [ctxLen, setCtxLen] = useState(8192);
  const forwards = useForwards(name);
  const api = forwards.find((f) => f.remotePort === OLLAMA_PORT);
  const cells = useStore(cellsStore)[name] ?? [];
  const deploy = [...cells].reverse().find((c) => c.kind === "deploy");
  const deploying = deploy?.status === "running";

  async function refresh() {
    setLoading(true);
    try {
      const st = await Colab.getLLMStatus(name);
      setStatus(st);
      if (st.context) setCtxLen(st.context);
    } catch (e) {
      toast("error", "读取 Ollama 状态失败", errorMessage(e));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, [name]); // eslint-disable-line react-hooks/exhaustive-deps

  const target = source === "file" ? fileModel.trim() : model.trim();

  async function start() {
    const opts = { model: target, path: source === "file" ? path.trim() : "", context: ctxLen };
    const ok = await runCell(name, { kind: "deploy", title: `部署 ${target}`, code: target }, (ch) => Colab.llmDeploy(name, opts, ch));
    if (ok) {
      if (!api) await openForward(name, OLLAMA_PORT, "Ollama API");
      toast("success", `${target} 已就绪`, "OpenAI 兼容 API 已转发到本机");
    }
    void refresh();
  }

  const loaded = status?.models.filter((m) => m.loaded) ?? [];
  const current = loaded[0]?.name ?? status?.models[0]?.name ?? "";

  return (
    <div className="h-full overflow-y-auto px-6 py-5">
      <div className="flex flex-col gap-4">
        <Card className="p-4">
          <div className="flex items-center gap-2">
            <Bot className="size-4 text-accent" />
            <div className="text-[13px] font-semibold">Ollama 服务</div>
            {status === null ? (
              <Spinner />
            ) : status.running ? (
              <Badge tone="success">运行中 {status.version}</Badge>
            ) : (
              <Badge>{status.installed ? "未运行" : "未安装"}</Badge>
            )}
            {status?.running && status.context > 0 && <span className="text-[11px] text-muted-foreground">上下文 {status.context}</span>}
            <div className="ml-auto flex gap-1.5">
              <Button size="sm" icon={<RefreshCw className="size-3.5" />} loading={loading} onClick={refresh}>
                刷新
              </Button>
              {status?.running && (
                <Button
                  size="sm"
                  variant="danger"
                  icon={<Power className="size-3.5" />}
                  onClick={async () => {
                    try {
                      await Colab.stopLLM(name);
                      if (api) await Colab.removeForward(api.id);
                      toast("success", "Ollama 已停止", "显存已释放");
                    } catch (e) {
                      toast("error", "停止失败", errorMessage(e));
                    }
                    void refresh();
                  }}
                >
                  停止服务
                </Button>
              )}
            </div>
          </div>
          {status && status.models.length > 0 && (
            <div className="mt-3 flex flex-col gap-1">
              {status.models.map((m) => (
                <div key={m.name} className="flex items-center gap-2 text-[12px]">
                  <span className="font-mono">{m.name}</span>
                  <span className="text-muted-foreground">{bytes(m.size)}</span>
                  {m.loaded && <Badge tone="success">{m.vram > 0 ? `已加载 · 显存 ${bytes(m.vram)}` : "已加载 · CPU"}</Badge>}
                  <button
                    className="ml-auto text-muted-foreground hover:text-danger"
                    title="从 VM 删除"
                    onClick={async () => {
                      try {
                        await Colab.removeLLMModel(name, m.name);
                      } catch (e) {
                        toast("error", "删除失败", errorMessage(e));
                      }
                      void refresh();
                    }}
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                </div>
              ))}
            </div>
          )}
          {status?.running && !api && (
            <div className="mt-3">
              <Button size="sm" variant="accent" onClick={() => openForward(name, OLLAMA_PORT, "Ollama API")}>
                转发到本机
              </Button>
            </div>
          )}
        </Card>

        {api && current && <Usage port={api.localPort} model={current} />}
        {api && <ForwardRow f={api} />}

        <Card className="p-4">
          <div className="text-[13px] font-semibold">部署模型</div>
          <div className="mt-1 text-[11px] text-muted-foreground">
            在这台 VM 上用 Ollama 运行模型，得到 OpenAI 兼容的 API，可接入任何支持自定义 Base URL 的客户端。
            {vram === 0 && <span className="text-warning"> 这是 CPU 会话：只适合测试 1–4B 的小模型。</span>}
          </div>
          <div className="mt-3">
            <Segmented
              value={source}
              onChange={setSource}
              options={[
                { value: "library", label: "Ollama 模型库" },
                { value: "hf", label: "HuggingFace GGUF" },
                { value: "file", label: "VM / Drive 上的文件" },
              ]}
            />
          </div>

          {source === "library" && (
            <div className="mt-3">
              <div className="flex flex-wrap gap-1.5">
                {PRESETS.map((p) => {
                  const gb = estimateGB(p.params);
                  const fits = vram === 0 ? p.params <= 4 : gb <= vram * 0.9;
                  return (
                    <button
                      key={p.model}
                      onClick={() => setModel(p.model)}
                      className={cx(
                        "rounded-lg border px-2.5 py-1 text-left text-[12px]",
                        model === p.model ? "border-accent bg-accent-soft" : "border-border hover:bg-muted",
                        !fits && "opacity-50",
                      )}
                    >
                      <span className="font-mono">{p.model}</span>
                      <span className="ml-1.5 text-[11px] text-muted-foreground">约 {gb.toFixed(0)} GB</span>
                    </button>
                  );
                })}
              </div>
              <input className={cx(inputClass, "mt-2 font-mono")} value={model} onChange={(e) => setModel(e.target.value.trim())} placeholder="模型名，例如 qwen3:8b" />
              <div className="mt-1 text-[11px] text-muted-foreground">名字见 ollama.com/library。大小按 4-bit 量化估算。</div>
            </div>
          )}

          {source === "hf" && <HFPicker vram={vram} onPick={setModel} picked={model} />}

          {source === "file" && (
            <div className="mt-3 flex flex-col gap-2">
              <input className={cx(inputClass, "font-mono")} value={path} onChange={(e) => setPath(e.target.value)} placeholder="/content/drive/MyDrive/models/xxx.gguf" />
              <input
                className={cx(inputClass, "font-mono")}
                value={fileModel}
                onChange={(e) => setFileModel(e.target.value.trim())}
                placeholder={(path.split("/").pop() ?? "").replace(/\.gguf$/i, "").toLowerCase() || "模型名"}
              />
              <div className="text-[11px] text-muted-foreground">Drive 上的文件需要先挂载 Drive（终端 → 挂载 Google Drive）。第二栏是在 API 里用的模型名。</div>
            </div>
          )}

          <div className="mt-3 flex items-center gap-2">
            <select className={cx(inputClass, "w-44")} value={ctxLen} onChange={(e) => setCtxLen(Number(e.target.value))}>
              {CONTEXTS.map((c) => (
                <option key={c} value={c}>
                  上下文 {c / 1024}K
                </option>
              ))}
            </select>
            <div className="flex-1" />
            {deploying ? (
              <Button icon={<Square className="size-3.5" />} onClick={() => deploy?.abort?.abort()}>
                取消
              </Button>
            ) : (
              <Button
                variant="accent"
                icon={<Rocket className="size-3.5" />}
                disabled={!target || (source === "file" && !path.trim().toLowerCase().endsWith(".gguf"))}
                onClick={() => {
                  if (source === "file" && !fileModel.trim()) setFileModel((path.split("/").pop() ?? "").replace(/\.gguf$/i, "").toLowerCase());
                  void start();
                }}
              >
                部署 {target}
              </Button>
            )}
          </div>
          {deploy && (
            <div className="mt-3 max-h-64 overflow-auto rounded-lg border border-border bg-console p-3">
              <Output chunks={deploy.chunks} />
              {deploy.error && <div className="ansi-out selectable mt-1 text-danger">{deploy.error}</div>}
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}

function HFPicker({ vram, onPick, picked }: { vram: number; onPick: (m: string) => void; picked: string }) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<HFModel[] | null>(null);
  const [repo, setRepo] = useState("");
  const [files, setFiles] = useState<HFFile[] | null>(null);
  const [busy, setBusy] = useState(false);

  async function search() {
    setBusy(true);
    try {
      setResults(await Colab.hfSearch(query, "gguf", "downloads"));
    } catch (e) {
      toast("error", "搜索失败", errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function open(id: string) {
    setRepo(id);
    setFiles(null);
    try {
      setFiles((await Colab.hfModelInfo(id)).files.filter(usableGGUF));
    } catch (e) {
      toast("error", "读取模型文件失败", errorMessage(e));
    }
  }

  return (
    <div className="mt-3">
      <div className="flex gap-2">
        <input
          className={inputClass}
          value={query}
          placeholder="搜索 GGUF 模型，例如 qwen3 gguf"
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void search()}
        />
        <Button icon={<Search className="size-3.5" />} loading={busy} onClick={search}>
          搜索
        </Button>
      </div>
      {results && (
        <div className="mt-2 flex max-h-44 flex-col overflow-y-auto rounded-lg border border-border">
          {results.length === 0 && <div className="p-2.5 text-[12px] text-muted-foreground">没有结果</div>}
          {results.map((m) => (
            <button
              key={m.id}
              onClick={() => void open(m.id)}
              className={cx("flex items-center gap-2 border-b border-border px-2.5 py-1.5 text-left text-[12px] last:border-0", repo === m.id ? "bg-accent-soft" : "hover:bg-muted")}
            >
              <span className="truncate font-mono">{m.id}</span>
              <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">↓ {m.downloads.toLocaleString()}</span>
            </button>
          ))}
        </div>
      )}
      {repo && (
        <div className="mt-2 rounded-lg border border-border p-2">
          {files === null ? (
            <Spinner />
          ) : files.length === 0 ? (
            <div className="text-[12px] text-muted-foreground">这个仓库没有可用的单文件 GGUF（分片文件 Ollama 不能直接拉取）。</div>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {files.map((f) => {
                const q = quantOf(f.name);
                const ref = q ? `hf.co/${repo}:${q}` : "";
                const gb = f.size / 1e9 + 1;
                const fits = vram === 0 ? gb <= 4 : gb <= vram * 0.9;
                return (
                  <button
                    key={f.name}
                    disabled={!q}
                    title={f.name}
                    onClick={() => onPick(ref)}
                    className={cx(
                      "rounded-lg border px-2.5 py-1 text-[12px] disabled:opacity-40",
                      picked === ref ? "border-accent bg-accent-soft" : "border-border hover:bg-muted",
                      !fits && "opacity-50",
                    )}
                  >
                    <span className="font-mono">{q ?? f.name}</span>
                    <span className="ml-1.5 text-[11px] text-muted-foreground">{bytes(f.size)}</span>
                  </button>
                );
              })}
            </div>
          )}
        </div>
      )}
      {picked.startsWith("hf.co/") && <div className="mt-2 font-mono text-[11px] text-muted-foreground">{picked}</div>}
    </div>
  );
}

function Usage({ port, model }: { port: number; model: string }) {
  const base = `http://127.0.0.1:${port}/v1`;
  const curl = `curl ${base}/chat/completions -H "Content-Type: application/json" -d '{"model":"${model}","messages":[{"role":"user","content":"你好"}]}'`;
  const py = `from openai import OpenAI\nclient = OpenAI(base_url="${base}", api_key="ollama")\nr = client.chat.completions.create(model="${model}", messages=[{"role": "user", "content": "你好"}])\nprint(r.choices[0].message.content)`;
  const rows: [string, string][] = [
    ["Base URL", base],
    ["API Key", "ollama（任意值）"],
    ["模型", model],
  ];
  return (
    <Card className="p-4">
      <div className="text-[13px] font-semibold">OpenAI 兼容 API</div>
      <div className="mt-2 grid grid-cols-[80px_1fr_auto] items-center gap-x-2 gap-y-1">
        {rows.map(([k, v]) => (
          <Row key={k} k={k} v={v} />
        ))}
      </div>
      <div className="mt-3 flex gap-1.5">
        <Button size="sm" icon={<Copy className="size-3.5" />} onClick={() => copyText(curl)}>
          复制 curl
        </Button>
        <Button size="sm" icon={<Copy className="size-3.5" />} onClick={() => copyText(py)}>
          复制 Python
        </Button>
      </div>
      <ChatTest port={port} model={model} />
    </Card>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  const copy = v.split("（")[0] ?? v;
  return (
    <>
      <span className="text-[11px] text-muted-foreground">{k}</span>
      <span className="truncate font-mono text-[12px]">{v}</span>
      <button className="text-muted-foreground hover:text-foreground" title="复制" onClick={() => copyText(copy)}>
        <Copy className="size-3.5" />
      </button>
    </>
  );
}

function ChatTest({ port, model }: { port: number; model: string }) {
  const [prompt, setPrompt] = useState("用一句话介绍你自己。");
  const [thinking, setThinking] = useState("");
  const [answer, setAnswer] = useState("");
  const [busy, setBusy] = useState(false);
  const abort = useRef<AbortController | null>(null);
  const stats = useMemo(() => answer.match(/\n\n(\d+ tokens · [\d.]+ tokens\/s)\n$/)?.[1], [answer]);

  async function send() {
    const ac = new AbortController();
    abort.current = ac;
    setBusy(true);
    setThinking("");
    setAnswer("");
    try {
      await streamCall(
        (ch) => Colab.llmChat(port, model, prompt, ch),
        (c) => (c.stream === "info" ? setThinking((t) => t + (c.text ?? "")) : setAnswer((a) => a + (c.text ?? ""))),
        ac.signal,
      );
    } catch (e) {
      if (!isAbort(e)) toast("error", "请求失败", errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  const tokenLine = thinking.match(/(\d+ tokens · [\d.]+ tokens\/s)\s*$/)?.[1] ?? stats;
  const think = thinking.replace(/\n*\d+ tokens · [\d.]+ tokens\/s\s*$/, "").trim();

  return (
    <div className="mt-4 border-t border-border pt-3">
      <div className="flex gap-2">
        <input className={inputClass} value={prompt} onChange={(e) => setPrompt(e.target.value)} onKeyDown={(e) => e.key === "Enter" && !busy && void send()} />
        {busy ? (
          <Button icon={<Square className="size-3.5" />} onClick={() => abort.current?.abort()}>
            停止
          </Button>
        ) : (
          <Button icon={<Send className="size-3.5" />} disabled={!prompt.trim()} onClick={send}>
            测试
          </Button>
        )}
      </div>
      {(think || answer || busy) && (
        <div className="mt-2 rounded-lg border border-border bg-console p-3 text-[13px]">
          {think && <div className="selectable mb-2 whitespace-pre-wrap text-[12px] text-muted-foreground">{think}</div>}
          <div className="selectable whitespace-pre-wrap">{answer.trim() || (busy ? <Spinner /> : null)}</div>
          {tokenLine && <div className="mt-2 text-[11px] text-muted-foreground">{tokenLine}</div>}
        </div>
      )}
    </div>
  );
}
