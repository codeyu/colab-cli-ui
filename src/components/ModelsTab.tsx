import { useEffect, useMemo, useRef, useState } from "react";
import { Download, ExternalLink, Heart, Lock, Search, SquareCode, Square } from "lucide-react";
import { Colab, errorMessage, type HFModel, type HFModelDetail, type Session } from "../lib/api";
import { acceleratorInfo } from "../lib/accelerators";
import { bytes, cx } from "../lib/format";
import { cellsStore, toast, useStore } from "../lib/store";
import { runCell } from "./ConsoleTab";
import { Output } from "./Output";
import { Button, Empty, IconButton, Spinner, inputClass } from "./ui";

const TASKS = [
  { value: "", label: "全部" },
  { value: "text-generation", label: "文本生成" },
  { value: "image-text-to-text", label: "多模态" },
  { value: "text-to-image", label: "文生图" },
  { value: "text-to-video", label: "文生视频" },
  { value: "automatic-speech-recognition", label: "语音识别" },
  { value: "text-to-speech", label: "语音合成" },
  { value: "feature-extraction", label: "向量" },
  { value: "gguf", label: "GGUF" },
];

const SORTS = [
  { value: "downloads", label: "下载量" },
  { value: "trendingScore", label: "趋势" },
  { value: "likes", label: "点赞" },
  { value: "lastModified", label: "最近更新" },
];

// Kept while the app runs, so switching tabs keeps the search.
const memo: { query: string; task: string; sort: string; results: HFModel[] | null; selected: string | null } = {
  query: "",
  task: "text-generation",
  sort: "downloads",
  results: null,
  selected: null,
};

function compact(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}K`;
  return String(n);
}

// Other frameworks' copies of the weights, which PyTorch on Colab does not need.
const OTHER_FORMATS = /^(original|onnx|openvino|coreml|mlx|tflite)\/|\.(tflite|h5|msgpack|ot|onnx|onnx_data|mlmodel)$/i;

function defaultSelection(d: HFModelDetail): Set<string> {
  let files = d.files.filter((f) => f.name !== ".gitattributes" && !OTHER_FORMATS.test(f.name));
  // Safetensors make the pickled copies of the same weights redundant.
  if (files.some((f) => f.name.endsWith(".safetensors"))) files = files.filter((f) => !/\.(bin|pt|pth|ckpt)$/i.test(f.name));
  const ggufs = files.filter((f) => f.name.toLowerCase().endsWith(".gguf"));
  if (ggufs.length <= 1) return new Set(files.map((f) => f.name));
  const pick = ggufs.find((f) => /q4_k_m/i.test(f.name)) ?? ggufs[0]!;
  return new Set(files.filter((f) => !f.name.toLowerCase().endsWith(".gguf") || f === pick).map((f) => f.name));
}

function loadSnippet(d: HFModelDetail, dest: string, files: string[]): string {
  const gguf = files.find((f) => f.toLowerCase().endsWith(".gguf"));
  if (gguf) {
    return `# pip install llama-cpp-python\nfrom llama_cpp import Llama\nllm = Llama(model_path=${JSON.stringify(`${dest}/${gguf}`)}, n_gpu_layers=-1, n_ctx=8192)\nprint(llm.create_chat_completion(messages=[{"role": "user", "content": "你好"}])["choices"][0]["message"]["content"])`;
  }
  if (d.libraryName === "diffusers") {
    return `import torch\nfrom diffusers import DiffusionPipeline\npipe = DiffusionPipeline.from_pretrained(${JSON.stringify(dest)}, torch_dtype=torch.bfloat16).to("cuda")`;
  }
  if (d.libraryName === "sentence-transformers") {
    return `from sentence_transformers import SentenceTransformer\nmodel = SentenceTransformer(${JSON.stringify(dest)}, device="cuda")\nprint(model.encode(["你好"]).shape)`;
  }
  if (d.pipelineTag === "text-generation") {
    return `from transformers import AutoModelForCausalLM, AutoTokenizer\ntok = AutoTokenizer.from_pretrained(${JSON.stringify(dest)})\nmodel = AutoModelForCausalLM.from_pretrained(${JSON.stringify(dest)}, torch_dtype="auto", device_map="auto")\nmsgs = [{"role": "user", "content": "你好"}]\nids = tok.apply_chat_template(msgs, add_generation_prompt=True, return_tensors="pt").to(model.device)\nprint(tok.decode(model.generate(ids, max_new_tokens=256)[0][ids.shape[1]:], skip_special_tokens=True))`;
  }
  if (d.pipelineTag) {
    return `from transformers import pipeline\npipe = pipeline(${JSON.stringify(d.pipelineTag)}, model=${JSON.stringify(dest)}, torch_dtype="auto", device_map="auto")`;
  }
  return `import os\nprint(os.listdir(${JSON.stringify(dest)}))`;
}

export function ModelsTab({ session, onUseInConsole }: { session: Session | null; onUseInConsole?: (code: string) => void }) {
  const [query, setQuery] = useState(memo.query);
  const [task, setTask] = useState(memo.task);
  const [sort, setSort] = useState(memo.sort);
  const [results, setResults] = useState<HFModel[] | null>(memo.results);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(memo.selected);
  const seq = useRef(0);

  useEffect(() => {
    Object.assign(memo, { query, task, sort, results, selected });
  });

  useEffect(() => {
    const n = ++seq.current;
    const t = setTimeout(
      async () => {
        setSearching(true);
        setError(null);
        try {
          const r = await Colab.hfSearch(query, task, sort);
          if (n === seq.current) setResults(r);
        } catch (e) {
          if (n === seq.current) setError(errorMessage(e));
        } finally {
          if (n === seq.current) setSearching(false);
        }
      },
      results === null ? 0 : 350,
    );
    return () => clearTimeout(t);
  }, [query, task, sort]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="flex h-full min-h-0">
      <div className="flex w-[360px] shrink-0 flex-col border-r border-border">
        <div className="flex flex-col gap-2 border-b border-border p-3">
          <div className="relative">
            <Search className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
            <input className={cx(inputClass, "pl-8")} value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索 HuggingFace 模型" spellCheck={false} autoFocus />
          </div>
          <div className="flex gap-2">
            <select className={cx(inputClass, "h-7 text-xs")} value={task} onChange={(e) => setTask(e.target.value)}>
              {TASKS.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </select>
            <select className={cx(inputClass, "h-7 w-28 text-xs")} value={sort} onChange={(e) => setSort(e.target.value)}>
              {SORTS.map((t) => (
                <option key={t.value} value={t.value}>
                  {t.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
          {searching && results === null && <Spinner className="m-3" />}
          {error && <div className="p-3 text-xs break-words text-danger">{error}</div>}
          {results?.length === 0 && <div className="p-3 text-xs text-muted-foreground">没有结果</div>}
          {results?.map((m) => {
            const [org, name] = m.id.includes("/") ? m.id.split("/", 2) : ["", m.id];
            return (
              <button
                key={m.id}
                onClick={() => setSelected(m.id)}
                className={cx("block w-full rounded-lg px-2.5 py-1.5 text-left", selected === m.id ? "bg-muted" : "hover:bg-muted/60", searching && "opacity-60")}
              >
                <div className="truncate text-[13px]">
                  {org && <span className="text-muted-foreground">{org}/</span>}
                  <span className="font-medium">{name}</span>
                </div>
                <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                  {m.pipelineTag && <span className="truncate">{m.pipelineTag}</span>}
                  <span className="flex items-center gap-0.5">
                    <Download className="size-3" />
                    {compact(m.downloads)}
                  </span>
                  <span className="flex items-center gap-0.5">
                    <Heart className="size-3" />
                    {compact(m.likes)}
                  </span>
                  {m.gated && <Lock className="size-3" />}
                </div>
              </button>
            );
          })}
        </div>
      </div>
      <div className="min-w-0 flex-1">
        {selected ? (
          <ModelDetail key={selected} id={selected} session={session} onUseInConsole={onUseInConsole} />
        ) : (
          <Empty icon={<Search className="size-5" />} title="选择一个模型" />
        )}
      </div>
    </div>
  );
}

function ModelDetail({ id, session, onUseInConsole }: { id: string; session: Session | null; onUseInConsole?: (code: string) => void }) {
  const [d, setD] = useState<HFModelDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [dest, setDest] = useState(`/content/models/${id.split("/").pop()}`);
  const [hasToken, setHasToken] = useState<boolean | null>(null);
  const cells = (useStore(cellsStore)[session?.name ?? ""] ?? []).filter((c) => c.kind === "download" && c.code === id);
  const last = cells[cells.length - 1];
  const vram = session ? (acceleratorInfo(session.hardware)?.vramGB ?? 0) : 0;

  useEffect(() => {
    Colab.hfModelInfo(id)
      .then((m) => {
        setD(m);
        setPicked(defaultSelection(m));
      })
      .catch((e) => setError(errorMessage(e)));
    Colab.getHFToken().then((t) => setHasToken(t.set));
  }, [id]);

  const files = useMemo(() => d?.files.filter((f) => f.name !== ".gitattributes") ?? [], [d]);
  const total = files.filter((f) => picked.has(f.name)).reduce((s, f) => s + f.size, 0);
  const weights = files
    .filter((f) => picked.has(f.name) && /\.(safetensors|bin|gguf|pt|pth|ckpt)$/i.test(f.name))
    .reduce((s, f) => s + f.size, 0);
  const tooBig = vram > 0 && weights > vram * 1024 ** 3 * 0.9;

  if (error) return <div className="p-6 text-xs text-danger">{error}</div>;
  if (!d) return <Spinner className="m-6" />;

  function download() {
    if (!session) return;
    const list = files.filter((f) => picked.has(f.name)).map((f) => f.name);
    void runCell(session.name, { kind: "download", title: `下载 ${id}`, code: id }, (ch) =>
      Colab.hfDownload(session.name, { repo: id, files: list, dest, totalBytes: total }, ch),
    ).then((ok) => ok && toast("success", "模型已下载到 VM", dest));
  }

  const running = last?.status === "running";

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-b border-border px-5 py-3">
        <div className="flex items-center gap-2">
          <span className="truncate text-[15px] font-semibold">{d.id}</span>
          <IconButton label="在 HuggingFace 打开" className="size-6" onClick={() => Colab.openExternal(`https://huggingface.co/${d.id}`)}>
            <ExternalLink className="size-3.5" />
          </IconButton>
        </div>
        <div className="mt-0.5 flex flex-wrap gap-x-2.5 text-[11px] text-muted-foreground">
          {d.parameters > 0 && <span>{compact(d.parameters)} 参数</span>}
          {Object.keys(d.dtypes).length > 0 && <span>{Object.keys(d.dtypes).join("/")}</span>}
          {d.libraryName && <span>{d.libraryName}</span>}
          {d.license && <span>{d.license}</span>}
          {weights > 0 && (
            <span className={cx(tooBig && "text-warning")}>
              权重 {bytes(weights)}
              {vram > 0 ? ` · 显存 ${vram} GB` : ""}
            </span>
          )}
          {d.gated && <span className={cx(hasToken === false && "text-warning")}>需授权{hasToken === false ? "（设置中添加 HF token）" : ""}</span>}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
        <label className="flex h-7 items-center gap-2.5 px-2 text-[11px] text-muted-foreground">
          <input
            type="checkbox"
            className="accent-[var(--accent)]"
            checked={picked.size === files.length}
            onChange={(e) => setPicked(e.target.checked ? new Set(files.map((f) => f.name)) : new Set())}
          />
          {picked.size}/{files.length} 个文件 · {bytes(total)}
        </label>
        {files.map((f) => (
          <label key={f.name} className="flex h-7 cursor-default items-center gap-2.5 rounded-md px-2 hover:bg-muted/60">
            <input
              type="checkbox"
              className="accent-[var(--accent)]"
              checked={picked.has(f.name)}
              onChange={(e) =>
                setPicked((s) => {
                  const n = new Set(s);
                  if (e.target.checked) n.add(f.name);
                  else n.delete(f.name);
                  return n;
                })
              }
            />
            <span className={cx("min-w-0 flex-1 truncate font-mono text-xs", !picked.has(f.name) && "text-muted-foreground")}>{f.name}</span>
            <span className="text-[11px] text-muted-foreground tabular-nums">{bytes(f.size)}</span>
          </label>
        ))}
      </div>

      {last && (
        <div className="max-h-48 overflow-auto border-t border-border bg-console px-5 py-2">
          <Output chunks={last.chunks} hideInfo />
          {last.error && <div className="ansi-out text-danger">{last.error}</div>}
        </div>
      )}

      {session && onUseInConsole && (
      <div className="flex items-center gap-2 border-t border-border px-5 py-3">
        <input className={cx(inputClass, "font-mono text-xs")} value={dest} onChange={(e) => setDest(e.target.value)} spellCheck={false} />
        <Button
          icon={<SquareCode className="size-3.5" />}
          onClick={() => onUseInConsole(loadSnippet(d, dest, files.filter((f) => picked.has(f.name)).map((f) => f.name)))}
        >
          加载代码
        </Button>
        {running ? (
          <Button variant="danger" icon={<Square className="size-3.5" />} onClick={() => last?.abort?.abort()}>
            停止
          </Button>
        ) : (
          <Button variant="accent" icon={<Download className="size-3.5" />} disabled={picked.size === 0 || !dest.trim()} onClick={download}>
            下载到 VM
          </Button>
        )}
      </div>
      )}
    </div>
  );
}
