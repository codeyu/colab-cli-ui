import { useEffect, useRef, useState } from "react";
import {
  CheckCircle2,
  Copy,
  FileCode2,
  Play,
  RotateCcw,
  Square,
  Terminal,
  Trash2,
  XCircle,
} from "lucide-react";
import type { Channel } from "mygo-runtime";
import { Colab, errorMessage, isAbort, streamCall, type Chunk } from "../lib/api";
import { cx, duration } from "../lib/format";
import { notifyDone } from "../lib/notify";
import { addCell, appendChunk, cellsStore, clearCells, toast, type Cell, updateCell, useStore } from "../lib/store";
import { Output } from "./Output";
import { Button, Empty, IconButton, Segmented, Spinner } from "./ui";

/**
 * Runs a streaming task as a console cell of the session. A long task that
 * ends while the window is in the background shows a notification, unless
 * notify is false (a step of a longer run that notifies for itself).
 */
export async function runCell(
  session: string,
  meta: Omit<Cell, "id" | "chunks" | "status" | "started" | "abort">,
  start: (ch: Channel<Chunk>) => Promise<void>,
  notify = true,
): Promise<boolean> {
  const started = Date.now();
  const ac = new AbortController();
  const id = addCell(session, { ...meta, abort: ac });
  // Stopping only disconnects colab exec; the code would run on in the
  // kernel, so interrupt it too.
  if (meta.kind === "python" || meta.kind === "shell" || meta.kind === "file") {
    ac.signal.addEventListener(
      "abort",
      () => void Colab.interruptKernel(session).catch((e) => toast("error", "代码可能仍在 VM 上运行", errorMessage(e))),
      { once: true },
    );
  }
  try {
    await streamCall(
      start,
      (c) => appendChunk(session, id, c),
      ac.signal,
    );
    updateCell(session, id, (c) => ({ ...c, status: "done", ended: Date.now(), abort: undefined }));
    if (notify) notifyDone(`${meta.title}：完成`, `${session} · 用时 ${duration((Date.now() - started) / 1000)}`, started);
    return true;
  } catch (e) {
    if (notify && !isAbort(e)) notifyDone(`${meta.title}：出错`, `${session} · ${errorMessage(e)}`, started);
    updateCell(session, id, (c) => ({
      ...c,
      status: isAbort(e) ? "canceled" : "error",
      error: isAbort(e) ? undefined : errorMessage(e),
      ended: Date.now(),
      abort: undefined,
    }));
    return false;
  }
}

const drafts: Record<string, { code: string; mode: "python" | "shell" }> = {};

/** Puts code in a session's console input, for the console to show next. */
export function setConsoleDraft(session: string, code: string) {
  drafts[session] = { code, mode: "python" };
}

const SNIPPETS: { label: string; mode: "python" | "shell"; code: string }[] = [
  { label: "GPU 信息", mode: "shell", code: "nvidia-smi" },
  { label: "磁盘与内存", mode: "shell", code: "df -h /content && free -h" },
  { label: "PyTorch 检查", mode: "python", code: "import torch\nprint(torch.__version__, torch.cuda.is_available())\nif torch.cuda.is_available():\n    print(torch.cuda.get_device_name(0))" },
  { label: "matplotlib 绘图", mode: "python", code: "import matplotlib.pyplot as plt\nimport numpy as np\nx = np.linspace(0, 10, 200)\nplt.plot(x, np.sin(x))\nplt.title('sin(x)')\nplt.show()" },
];

export function ConsoleTab({ session }: { session: string }) {
  const all = useStore(cellsStore);
  const cells = all[session] ?? [];
  const draft = drafts[session] ?? { code: "", mode: "python" as const };
  const [code, setCode] = useState(draft.code);
  const [mode, setMode] = useState<"python" | "shell">(draft.mode);
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const running = cells.some((c) => c.status === "running");

  useEffect(() => {
    drafts[session] = { code, mode };
  }, [session, code, mode]);

  useEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [cells]);

  function run(source = code, m = mode) {
    if (!source.trim()) return;
    stick.current = true;
    void runCell(
      session,
      { kind: m, title: m === "shell" ? "Shell" : "Python", code: source },
      (ch) => Colab.exec(session, source, m === "shell", ch),
    );
  }

  async function runFile() {
    const file = await Colab.pickFile("script");
    if (!file) return;
    stick.current = true;
    void runCell(
      session,
      { kind: "file", title: file.split(/[\\/]/).pop() ?? file, code: file },
      (ch) => Colab.execFile(session, file, ch),
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        ref={scroller}
        onScroll={(e) => {
          const el = e.currentTarget;
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        }}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        {cells.length === 0 ? (
          <Empty icon={<Terminal className="size-5" />} title="在远程内核中运行代码">
            代码通过 <code className="font-mono">colab exec</code> 发送到这个会话的 Jupyter 内核。变量和导入会在多次执行之间保留；
            切换到 Shell 模式可以运行 bash 命令。
            <div className="mt-4 flex flex-wrap justify-center gap-1.5">
              {SNIPPETS.map((s) => (
                <Button
                  key={s.label}
                  size="sm"
                  onClick={() => {
                    setMode(s.mode);
                    setCode(s.code);
                  }}
                >
                  {s.label}
                </Button>
              ))}
            </div>
          </Empty>
        ) : (
          <div className="mx-auto flex max-w-[980px] flex-col gap-3 px-6 py-5">
            {cells.map((c) => (
              <CellView key={c.id} cell={c} onRerun={c.code && (c.kind === "python" || c.kind === "shell") ? () => run(c.code!, c.kind as "python" | "shell") : undefined} />
            ))}
          </div>
        )}
      </div>

      <div className="border-t border-border bg-background px-6 pt-3 pb-4">
        <div className="mx-auto max-w-[980px]">
          <div className="rounded-xl border border-border bg-card shadow-sm focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/10">
            <textarea
              value={code}
              onChange={(e) => setCode(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.ctrlKey || e.metaKey || e.shiftKey)) {
                  e.preventDefault();
                  run();
                }
                if (e.key === "Tab") {
                  e.preventDefault();
                  const el = e.currentTarget;
                  const { selectionStart: s, selectionEnd: end } = el;
                  const next = code.slice(0, s) + "    " + code.slice(end);
                  setCode(next);
                  requestAnimationFrame(() => el.setSelectionRange(s + 4, s + 4));
                }
              }}
              spellCheck={false}
              placeholder={mode === "python" ? "print('Hello from Colab!')    — Ctrl+Enter 运行" : "nvidia-smi    — Ctrl+Enter 运行"}
              className="block max-h-[40vh] min-h-[88px] w-full resize-y bg-transparent px-3.5 pt-3 pb-1 font-mono text-[12.5px] leading-relaxed outline-none placeholder:text-muted-foreground/60"
            />
            <div className="flex items-center gap-2 px-2 pb-2">
              <Segmented
                value={mode}
                onChange={setMode}
                options={[
                  { value: "python", label: "Python" },
                  { value: "shell", label: "Shell" },
                ]}
              />
              <Button size="sm" variant="ghost" icon={<FileCode2 className="size-3.5" />} onClick={runFile}>
                运行本地文件
              </Button>
              <div className="flex-1" />
              {cells.length > 0 && (
                <IconButton label="清空输出" onClick={() => clearCells(session)}>
                  <Trash2 className="size-3.5" />
                </IconButton>
              )}
              <Button size="sm" variant="accent" icon={<Play className="size-3.5" />} disabled={!code.trim()} onClick={() => run()}>
                运行{running ? "（排队）" : ""}
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function CellView({ cell, onRerun }: { cell: Cell; onRerun?: () => void }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (cell.status !== "running") return;
    const t = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(t);
  }, [cell.status]);
  const elapsed = ((cell.ended ?? now) - cell.started) / 1000;
  const hasOutput = cell.chunks.some((c) => c.stream !== "info" && (c.text || c.image));

  return (
    <div className="fade-in overflow-hidden rounded-xl border border-border bg-card">
      <div className="flex items-center gap-2 border-b border-border px-3 py-1.5">
        {cell.status === "running" && <Spinner className="text-accent" />}
        {cell.status === "done" && <CheckCircle2 className="size-3.5 text-success" />}
        {cell.status === "error" && <XCircle className="size-3.5 text-danger" />}
        {cell.status === "canceled" && <Square className="size-3.5 text-muted-foreground" />}
        <span className="text-xs font-medium">{cell.title}</span>
        <span className="text-[11px] text-muted-foreground tabular-nums">{duration(elapsed)}</span>
        <div className="flex-1" />
        {cell.code && (
          <IconButton label="复制代码" className="size-6" onClick={() => navigator.clipboard.writeText(cell.code!)}>
            <Copy className="size-3" />
          </IconButton>
        )}
        {onRerun && cell.status !== "running" && (
          <IconButton label="再次运行" className="size-6" onClick={onRerun}>
            <RotateCcw className="size-3" />
          </IconButton>
        )}
        {cell.status === "running" && (
          <Button size="sm" variant="ghost" className="h-6" icon={<Square className="size-3" />} onClick={() => cell.abort?.abort()}>
            停止
          </Button>
        )}
      </div>
      {cell.code && cell.kind !== "file" && (
        <pre className="selectable max-h-60 overflow-auto bg-muted/50 px-3.5 py-2.5 font-mono text-[12px] leading-relaxed">
          {cell.kind === "shell" && <span className="text-accent select-none">$ </span>}
          {cell.code}
        </pre>
      )}
      <div className={cx("px-3.5", (hasOutput || cell.error) && "py-2.5")}>
        <Output chunks={cell.chunks} hideInfo />
        {cell.error && <div className="ansi-out selectable mt-1 text-danger">{cell.error}</div>}
      </div>
    </div>
  );
}
