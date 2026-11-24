import { useState } from "react";
import {
  Coins,
  FileCode2,
  Gauge,
  Play,
  Plus,
  Power,
  Rocket,
  Server,
  Square,
  UserRound,
} from "lucide-react";
import { Colab, errorMessage, type Account, type Session, type Usage } from "../lib/api";
import { ACCELERATORS, costLine, eligibilityOf } from "../lib/accelerators";
import { cx, duration } from "../lib/format";
import { cellsStore, toast, useStore } from "../lib/store";
import { runCell } from "./ConsoleTab";
import { Output } from "./Output";
import { Badge, Button, Card, Dialog, Field, Spinner, StatusDot, Toggle, inputClass } from "./ui";

const RUN_KEY = "__run__";

export function HomeView({
  sessions,
  usage,
  account,
  onOpen,
  onNew,
  onChanged,
}: {
  sessions: Session[] | null;
  usage: Usage | null;
  account: Account | null;
  onOpen: (name: string) => void;
  onNew: () => void;
  onChanged: () => void;
}) {
  const [stopAll, setStopAll] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [orphan, setOrphan] = useState<Session | null>(null);
  const [releasing, setReleasing] = useState(false);

  async function releaseOrphan(s: Session) {
    setReleasing(true);
    try {
      await Colab.releaseOrphan(s.endpoint);
      toast("success", "已释放", s.endpoint);
    } catch (e) {
      toast("error", "释放失败", errorMessage(e));
    } finally {
      setReleasing(false);
      setOrphan(null);
      onChanged();
    }
  }
  const local = sessions?.filter((s) => s.local) ?? [];
  const hours = usage && usage.ratePerHour > 0 ? usage.balance / usage.ratePerHour : null;

  async function doStopAll() {
    setStopAll(false);
    setStopping(true);
    try {
      const out = await Colab.stopAll();
      toast("success", "已释放全部会话", out);
    } catch (e) {
      toast("error", "释放失败", errorMessage(e));
    } finally {
      setStopping(false);
      onChanged();
    }
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[1040px] px-8 py-7">
        <h1 className="text-xl font-semibold">概览</h1>
        <p className="mt-1 text-xs text-muted-foreground">用图形界面操作 google-colab-cli：申请 CPU / GPU / TPU 运行时、执行代码、管理文件，用完记得释放。</p>

        <div className="mt-5 grid grid-cols-4 gap-3">
          <Stat icon={<Coins className="size-4 text-accent" />} label="计算单元余额" value={usage ? usage.balance.toFixed(2) : "—"} unit="CU" />
          <Stat
            icon={<Gauge className="size-4" />}
            label="当前消耗"
            value={usage ? usage.ratePerHour.toFixed(2) : "—"}
            unit="CU/小时"
            sub={hours ? `按当前速率约可用 ${hours.toFixed(0)} 小时` : "当前没有计费中的运行时"}
          />
          <Stat icon={<Server className="size-4" />} label="活跃运行时" value={sessions ? String(sessions.length) : "—"} unit="个" sub={usage ? `服务器记录 ${usage.assignments} 个分配` : undefined} />
          <Stat icon={<UserRound className="size-4" />} label="账号" value={account?.email.split("@")[0] ?? "—"} sub={account ? `${account.provider} · 令牌剩余 ${account.expiresIn}` : "读取中…"} small />
        </div>

        <EligibilityPanel usage={usage} />

        <div className="mt-6 flex items-center justify-between">
          <h2 className="text-[13px] font-semibold">运行时</h2>
          <div className="flex gap-2">
            {local.length > 0 && (
              <Button size="sm" variant="danger" loading={stopping} icon={<Power className="size-3.5" />} onClick={() => setStopAll(true)}>
                全部释放
              </Button>
            )}
            <Button size="sm" variant="accent" icon={<Plus className="size-3.5" />} onClick={onNew}>
              新建会话
            </Button>
          </div>
        </div>
        <div className="mt-2.5 grid grid-cols-3 gap-3">
          {sessions === null && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Spinner /> 读取中…
            </div>
          )}
          {sessions?.length === 0 && (
            <button onClick={onNew} className="col-span-3 rounded-xl border border-dashed border-border p-6 text-xs text-muted-foreground hover:bg-muted/50">
              还没有运行时，点击这里申请一台
            </button>
          )}
          {sessions?.map((s) =>
            s.local ? (
              <button
                key={s.endpoint}
                onClick={() => onOpen(s.name)}
                className="rounded-xl border border-border bg-card p-3.5 text-left transition-colors hover:border-accent/40"
              >
                <div className="flex items-center gap-2">
                  <StatusDot tone={s.busy ? "warning" : "success"} pulse={s.busy} />
                  <span className="truncate text-[13px] font-semibold">{s.name}</span>
                  <Badge tone={s.hardware === "CPU" ? "neutral" : "accent"} className="ml-auto">
                    {s.hardware}
                  </Badge>
                </div>
                <div className="mt-1.5 truncate font-mono text-[11px] text-muted-foreground">{s.endpoint}</div>
                <div className="mt-1 text-[11px] text-muted-foreground">
                  {s.variant} · {s.shape} · {s.status || "IDLE"}
                </div>
              </button>
            ) : (
              <div key={s.endpoint} className="rounded-xl border border-dashed border-warning/50 bg-card p-3.5">
                <div className="flex items-center gap-2">
                  <StatusDot tone="warning" />
                  <span className="truncate text-[13px] font-semibold">未在本机登记</span>
                  <Badge tone={s.hardware === "CPU" ? "neutral" : "accent"} className="ml-auto">
                    {s.hardware}
                  </Badge>
                </div>
                <div className="mt-1.5 truncate font-mono text-[11px] text-muted-foreground">{s.endpoint}</div>
                <div className="mt-1 flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
                  <span className="truncate">仍在计费 · 网页或其它设备打开的</span>
                  <Button size="sm" variant="danger" className="h-6" onClick={() => setOrphan(s)}>
                    释放
                  </Button>
                </div>
              </div>
            ),
          )}
        </div>

        <RunJob usage={usage} />
      </div>

      <Dialog
        open={orphan !== null}
        onClose={() => !releasing && setOrphan(null)}
        title="释放这个运行时？"
        description={orphan ? `${orphan.hardware} · ${orphan.endpoint}：它不是由本机创建的（例如在网页里打开），释放后上面的文件和运行中的程序都会丢失。` : ""}
        width={460}
        footer={
          <>
            <Button variant="ghost" disabled={releasing} onClick={() => setOrphan(null)}>
              取消
            </Button>
            <Button variant="danger" loading={releasing} onClick={() => orphan && releaseOrphan(orphan)}>
              释放
            </Button>
          </>
        }
      >
        <div />
      </Dialog>
      <Dialog
        open={stopAll}
        onClose={() => setStopAll(false)}
        title="释放全部会话？"
        description={`将对本机创建的 ${local.length} 个会话执行 colab stop --all，内核状态与未保存的文件都会丢失。`}
        width={440}
        footer={
          <>
            <Button variant="ghost" onClick={() => setStopAll(false)}>
              取消
            </Button>
            <Button variant="danger" onClick={doStopAll}>
              全部释放
            </Button>
          </>
        }
      >
        <div />
      </Dialog>
    </div>
  );
}

function Stat({ icon, label, value, unit, sub, small }: { icon: React.ReactNode; label: string; value: string; unit?: string; sub?: string; small?: boolean }) {
  return (
    <Card className="p-4">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {icon}
        {label}
      </div>
      <div className="mt-2 flex items-baseline gap-1">
        <span className={cx("truncate font-semibold tabular-nums", small ? "text-[15px]" : "text-2xl")}>{value}</span>
        {unit && <span className="text-[11px] text-muted-foreground">{unit}</span>}
      </div>
      {sub && <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{sub}</div>}
    </Card>
  );
}

function splitArgs(s: string): string[] {
  const out: string[] = [];
  const re = /"([^"]*)"|'([^']*)'|(\S+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(s))) out.push(m[1] ?? m[2] ?? m[3] ?? "");
  return out;
}

function EligibilityPanel({ usage }: { usage: Usage | null }) {
  if (!usage?.eligibility.known) return null;
  return (
    <div className="mt-3 flex flex-wrap gap-x-2.5 text-[11px] text-muted-foreground">
      {ACCELERATORS.map((a) => (
        <span key={a.id} className={cx(eligibilityOf(usage, a.id) === "ineligible" && "opacity-35")}>
          {a.label.replace("TPU ", "")}
        </span>
      ))}
    </div>
  );
}

function RunJob({ usage }: { usage: Usage | null }) {
  const [script, setScript] = useState("");
  const [args, setArgs] = useState("");
  const [acc, setAcc] = useState("T4");
  const [highMem, setHighMem] = useState(false);
  const [keep, setKeep] = useState(false);
  const cells = useStore(cellsStore)[RUN_KEY] ?? [];
  const last = cells[cells.length - 1];
  const running = last?.status === "running";

  async function pick() {
    const f = await Colab.pickFile("py");
    if (f) setScript(f);
  }

  function run() {
    const opts = { script, args: splitArgs(args), accelerator: acc, highMem, keep, name: "" };
    void runCell(RUN_KEY, { kind: "run", title: script.split(/[\\/]/).pop() ?? script }, (ch) => Colab.runScript(opts, ch)).then((ok) =>
      ok ? toast("success", "任务完成", keep ? "虚拟机已保留，记得手动释放" : "虚拟机已自动释放") : undefined,
    );
  }

  return (
    <div className="mt-8">
      <div className="flex items-center gap-2">
        <Rocket className="size-4 text-accent" />
        <h2 className="text-[13px] font-semibold">一次性任务</h2>
        <span className="text-xs text-muted-foreground">colab run：新建虚拟机 → 运行本地脚本 → 自动释放</span>
      </div>
      <Card className="mt-2.5 p-4">
        <div className="grid grid-cols-[1fr_1fr] gap-3">
          <Field label="脚本">
            <div className="flex gap-2">
              <input className={inputClass} value={script} onChange={(e) => setScript(e.target.value)} placeholder="选择本地 .py 文件" />
              <Button icon={<FileCode2 className="size-3.5" />} onClick={pick}>
                浏览
              </Button>
            </div>
          </Field>
          <Field label="参数" hint="以 sys.argv 传给脚本">
            <input className={inputClass} value={args} onChange={(e) => setArgs(e.target.value)} placeholder='--epochs 3 --lr 1e-4' spellCheck={false} />
          </Field>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          {ACCELERATORS.map((a) => {
            const blocked = eligibilityOf(usage, a.id) === "ineligible";
            return (
              <button
                key={a.id}
                disabled={blocked}
                onClick={() => setAcc(a.id)}
                className={cx(
                  "h-7 rounded-lg border px-2.5 text-xs font-medium transition-colors",
                  blocked
                    ? "cursor-not-allowed border-border text-muted-foreground opacity-35"
                    : acc === a.id
                      ? "border-accent bg-accent-soft text-accent"
                      : "border-border text-muted-foreground hover:bg-muted",
                )}
              >
                {a.label}
              </button>
            );
          })}
        </div>
        <div className="mt-3 flex items-center gap-5">
          <Toggle checked={highMem} onChange={setHighMem} label="高内存" />
          <span className="text-[11px] text-muted-foreground">{costLine(acc, highMem, usage?.balance)}</span>
          <Toggle checked={keep} onChange={setKeep} label="完成后保留虚拟机（--keep）" />
          <div className="flex-1" />
          {running ? (
            <Button variant="danger" icon={<Square className="size-3.5" />} onClick={() => last?.abort?.abort()}>
              中止
            </Button>
          ) : (
            <Button variant="accent" icon={<Play className="size-3.5" />} disabled={!script} onClick={run}>
              开始运行
            </Button>
          )}
        </div>
        {last && (
          <div className="mt-4 rounded-lg border border-border bg-console">
            <div className="flex items-center gap-2 border-b border-border px-3 py-1.5 text-xs">
              {running && <Spinner className="text-accent" />}
              <span className="font-medium">{last.title}</span>
              <span className="text-muted-foreground">
                {running ? "运行中" : last.status === "done" ? "完成" : last.status === "canceled" ? "已中止" : "失败"} · {duration(((last.ended ?? Date.now()) - last.started) / 1000)}
              </span>
            </div>
            <div className="max-h-96 overflow-auto p-3">
              <Output chunks={last.chunks} />
              {last.error && <div className="ansi-out mt-1 text-danger">{last.error}</div>}
            </div>
          </div>
        )}
      </Card>
    </div>
  );
}
