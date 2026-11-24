import { useCallback, useEffect, useRef, useState } from "react";
import { ShieldCheck, ShieldOff, TimerReset } from "lucide-react";
import { Colab, errorMessage, type GuardStatus } from "../lib/api";
import { cx } from "../lib/format";
import { toast } from "../lib/store";
import { Button, Spinner, inputClass } from "./ui";

function hms(sec: number): string {
  sec = Math.max(0, Math.floor(sec));
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  return h ? `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}` : `${m}:${String(s).padStart(2, "0")}`;
}

/** The session's auto-release: a countdown, idle time, and its settings. */
export function GuardChip({ session }: { session: string }) {
  const [st, setSt] = useState<GuardStatus | null>(null);
  const [fetchedAt, setFetchedAt] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(Date.now());
  const [hours, setHours] = useState(0);
  const [idle, setIdle] = useState(60);
  const ref = useRef<HTMLDivElement>(null);

  const apply = useCallback((s: GuardStatus) => {
    setSt(s);
    setFetchedAt(Date.now());
    setError(null);
  }, []);

  const load = useCallback(async () => {
    try {
      apply(await Colab.getGuard(session));
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [session, apply]);

  // One reading, then a local countdown: each reading runs code in the
  // kernel, which the watchdog counts as activity.
  useEffect(() => {
    setSt(null);
    void load();
  }, [load]);

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    window.addEventListener("mousedown", close);
    return () => window.removeEventListener("mousedown", close);
  }, [open]);

  async function act(fn: () => Promise<GuardStatus>, ok: string) {
    setBusy(true);
    try {
      apply(await fn());
      toast("success", ok);
      setOpen(false);
    } catch (e) {
      toast("error", "设置自动释放失败", errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  // VM clock now, from the reading and the local time since.
  const vmNow = st ? st.vmNow + (now - fetchedAt) / 1000 : 0;
  const left = st && st.deadline ? st.deadline - vmNow : null;
  const idleFor = st && st.lastBusy ? vmNow - st.lastBusy : null;
  const on = !!st?.running;

  let label: React.ReactNode;
  if (!st && !error) label = <Spinner className="size-3" />;
  else if (error && !st) label = "自动释放：未知";
  else if (!on) label = "自动释放：关闭";
  else
    label = (
      <>
        {left !== null && <span className="tabular-nums">{hms(left)} 后释放</span>}
        {left !== null && st!.idleSeconds > 0 && <span className="text-muted-foreground"> · </span>}
        {st!.idleSeconds > 0 && (
          <span>
            空闲 {Math.round(st!.idleSeconds / 60)} 分钟释放
            {idleFor !== null && idleFor > 120 && <span className="text-muted-foreground">（已空闲 {Math.floor(idleFor / 60)} 分钟）</span>}
          </span>
        )}
      </>
    );

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => {
          if (st) {
            setHours(left !== null ? Math.max(1, Math.round(left / 3600)) : 0);
            setIdle(st.idleSeconds ? Math.round(st.idleSeconds / 60) : 0);
          }
          setOpen((o) => !o);
        }}
        title={error ?? st?.lastLog ?? ""}
        className={cx(
          "inline-flex h-6 items-center gap-1.5 rounded-md px-2 text-[11px] font-medium",
          on ? "bg-success/12 text-success" : "bg-warning/12 text-warning",
          left !== null && left < 600 && "bg-danger/12 text-danger",
        )}
      >
        {on ? <ShieldCheck className="size-3.5" /> : <ShieldOff className="size-3.5" />}
        {label}
      </button>
      {open && (
        <div className="fade-in absolute left-0 z-40 mt-1 w-72 rounded-xl border border-border bg-popover p-3 shadow-xl">
          {on && left !== null && (
            <div className="mb-3 flex gap-2">
              <Button size="sm" loading={busy} icon={<TimerReset className="size-3.5" />} onClick={() => act(() => Colab.extendGuard(session, 1), "已延长 1 小时")}>
                延长 1 小时
              </Button>
              <Button size="sm" loading={busy} onClick={() => act(() => Colab.extendGuard(session, 4), "已延长 4 小时")}>
                延长 4 小时
              </Button>
            </div>
          )}
          <div className="flex flex-col gap-2">
            <select className={inputClass} value={hours} onChange={(e) => setHours(Number(e.target.value))}>
              {[0, 1, 2, 4, 8, 12, 24].map((h) => (
                <option key={h} value={h}>
                  {h ? `从现在起最长 ${h} 小时` : "不限运行时长"}
                </option>
              ))}
            </select>
            <select className={inputClass} value={idle} onChange={(e) => setIdle(Number(e.target.value))}>
              {[0, 15, 30, 60, 120].map((m) => (
                <option key={m} value={m}>
                  {m ? `空闲 ${m} 分钟后释放` : "空闲不释放"}
                </option>
              ))}
            </select>
            <div className="flex justify-between gap-2">
              {on ? (
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => act(() => Colab.setGuard(session, 0, 0), "已关闭自动释放")}>
                  关闭
                </Button>
              ) : (
                <span />
              )}
              <Button size="sm" variant="accent" loading={busy} onClick={() => act(() => Colab.setGuard(session, hours, idle), "已更新自动释放")}>
                保存
              </Button>
            </div>
          </div>
          <div className="mt-2.5 text-[11px] leading-snug text-muted-foreground">由 VM 上的看门狗执行，关掉本软件也有效。空闲指 CPU、GPU 和网络都没有活动。</div>
        </div>
      )}
    </div>
  );
}
