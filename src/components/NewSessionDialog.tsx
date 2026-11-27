import { useEffect, useRef, useState } from "react";
import { Cpu, Server, Sparkles } from "lucide-react";
import { Colab, errorMessage, isAbort, streamCall, type Chunk, type StartupScript, type Usage } from "../lib/api";
import { runSteps } from "../lib/startup";
import { ACCELERATORS, costLine, eligibilityOf } from "../lib/accelerators";
import { cx, randomName } from "../lib/format";
import { toast } from "../lib/store";
import { Output } from "./Output";
import { Button, Dialog, Field, Toggle, inputClass } from "./ui";
import { RetrySelects } from "./SettingsView";

export function NewSessionDialog({
  open,
  onClose,
  onCreated,
  usage,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (name: string) => void;
  usage: Usage | null;
}) {
  const [name, setName] = useState(randomName());
  const [acc, setAcc] = useState("CPU");
  const [highMem, setHighMem] = useState(false);
  const [busy, setBusy] = useState(false);
  const [chunks, setChunks] = useState<Chunk[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [scripts, setScripts] = useState<StartupScript[]>([]);
  const [scriptId, setScriptId] = useState("");
  const [guardHours, setGuardHours] = useState(0);
  const [guardIdle, setGuardIdle] = useState(60);
  const [retries, setRetries] = useState(0);
  const [retryDelay, setRetryDelay] = useState(30);
  const abort = useRef<AbortController | null>(null);
  const info = ACCELERATORS.find((a) => a.id === acc)!;

  useEffect(() => {
    if (open && !busy) {
      setName(randomName());
      setChunks([]);
      setError(null);
      Colab.listScripts()
        .then(setScripts)
        .catch(() => setScripts([]));
      Colab.getSettings().then((s) => {
        setGuardHours(s.guardHours);
        setGuardIdle(s.guardIdleMinutes);
        setRetries(s.createRetries);
        setRetryDelay(s.createRetryDelay);
      });
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const valid = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$/.test(name);

  async function create() {
    setBusy(true);
    setError(null);
    setChunks([]);
    const ac = new AbortController();
    abort.current = ac;
    try {
      await streamCall(
        (ch) => Colab.createSession({ name, accelerator: acc, highMem: highMem && info.highMem, retries, retryDelay }, ch),
        (c) => setChunks((l) => [...l, c]),
        ac.signal,
      );
      toast("success", `会话 ${name} 已就绪`, `${info.label}${highMem && info.highMem ? " · 高内存" : ""}`);
      onCreated(name);
      onClose();
      const script = scripts.find((s) => s.id === scriptId);
      // The watchdog first, so the runtime is guarded while the script runs.
      void (async () => {
        if (guardHours > 0 || guardIdle > 0) {
          try {
            await Colab.setGuard(name, guardHours, guardIdle);
          } catch (e) {
            toast("error", "自动释放没有启动", errorMessage(e));
          }
        }
        if (script) await runSteps(name, script.steps.map((s) => s.code), { label: script.name, drive: script.drive });
      })();
    } catch (e) {
      setError(isAbort(e) ? "已取消申请" : errorMessage(e));
    } finally {
      setBusy(false);
      abort.current = null;
    }
  }

  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      title="新建 Colab 会话"
      description="申请一台新的虚拟机运行时（colab new）。GPU / TPU 会按小时消耗计算单元。"
      width={640}
      footer={
        <>
          {busy ? (
            <Button variant="ghost" onClick={() => abort.current?.abort()}>
              取消
            </Button>
          ) : (
            <Button variant="ghost" onClick={onClose}>
              关闭
            </Button>
          )}
          <Button variant="accent" loading={busy} disabled={!valid} onClick={create} icon={<Sparkles className="size-3.5" />}>
            {busy ? "正在申请…" : "创建会话"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="会话名称" hint="之后所有命令都用 -s 名称 指向这个会话">
          <input
            className={cx(inputClass, !valid && "border-danger/60")}
            value={name}
            onChange={(e) => setName(e.target.value.trim())}
            disabled={busy}
            autoFocus
          />
        </Field>

        <div>
          <div className="mb-1.5 text-xs font-medium">硬件</div>
          <div className="grid grid-cols-4 gap-2">
            {ACCELERATORS.map((a) => {
              const state = eligibilityOf(usage, a.id);
              const blocked = state === "ineligible";
              return (
                <button
                  key={a.id}
                  type="button"
                  disabled={busy || blocked}
                  onClick={() => setAcc(a.id)}
                  className={cx(
                    "flex flex-col items-start gap-0.5 rounded-lg border p-2.5 text-left transition-colors",
                    blocked
                      ? "cursor-not-allowed border-border opacity-35"
                      : acc === a.id
                        ? "border-accent bg-accent-soft"
                        : "border-border hover:bg-muted",
                  )}
                >
                  <div className="flex w-full items-center justify-between">
                    <span className="text-[13px] font-semibold">{a.label}</span>
                    {a.kind === "CPU" ? (
                      <Cpu className="size-3.5 text-muted-foreground" />
                    ) : (
                      <Server className={cx("size-3.5", acc === a.id ? "text-accent" : "text-muted-foreground")} />
                    )}
                  </div>
                  <span className="text-[11px] text-muted-foreground">
                    {a.kind} · {a.memory}
                  </span>
                </button>
              );
            })}
          </div>
          <div className="mt-2 text-[11px] text-muted-foreground">
            {info.note}
            <span className="ml-2 text-foreground">{costLine(acc, highMem && info.highMem, usage?.balance)}</span>
          </div>
        </div>

        <Field label="自动释放" hint="由 VM 上的看门狗执行，关掉本软件也有效">
          <div className="flex gap-2">
            <select className={inputClass} value={guardHours} onChange={(e) => setGuardHours(Number(e.target.value))} disabled={busy}>
              {[0, 1, 2, 4, 8, 12, 24].map((h) => (
                <option key={h} value={h}>
                  {h ? `最长运行 ${h} 小时` : "不限运行时长"}
                </option>
              ))}
            </select>
            <select className={inputClass} value={guardIdle} onChange={(e) => setGuardIdle(Number(e.target.value))} disabled={busy}>
              {[0, 15, 30, 60, 120].map((m) => (
                <option key={m} value={m}>
                  {m ? `空闲 ${m} 分钟后释放` : "空闲不释放"}
                </option>
              ))}
            </select>
          </div>
        </Field>

        <Field label="创建失败时" hint="只在 Colab 暂时没有资源或网络出错时重试，可以随时取消">
          <RetrySelects
            retries={retries}
            delay={retryDelay}
            disabled={busy}
            onChange={(r, d) => {
              setRetries(r);
              setRetryDelay(d);
            }}
          />
        </Field>

        {scripts.length > 0 && (
          <Field label="启动脚本" hint="会话就绪后自动执行，进度在控制台里">
            <select className={inputClass} value={scriptId} onChange={(e) => setScriptId(e.target.value)} disabled={busy}>
              <option value="">无</option>
              {scripts.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}（{s.steps.length} 步{s.drive ? " · Drive" : ""}）
                </option>
              ))}
            </select>
          </Field>
        )}

        <div className={cx(!info.highMem && "pointer-events-none opacity-50")}>
          <Toggle checked={highMem && info.highMem} onChange={setHighMem} label="高内存机型（--high-mem，需要 Pro / Pro+）" />
          {!info.highMem && <div className="mt-1 text-[11px] text-muted-foreground">该加速器只有一种机型。</div>}
        </div>

        {(chunks.length > 0 || error) && (
          <div className="max-h-48 overflow-auto rounded-lg border border-border bg-console p-3">
            <Output chunks={chunks} />
            {error && <div className="ansi-out selectable mt-1 text-danger">{error}</div>}
          </div>
        )}
      </div>
    </Dialog>
  );
}
