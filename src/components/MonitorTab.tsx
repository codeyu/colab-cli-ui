import { useCallback, useEffect, useRef, useState } from "react";
import { Activity, Cpu, Gauge, HardDrive, MemoryStick, RefreshCw, Thermometer, Zap } from "lucide-react";
import { Colab, errorMessage, type Stats } from "../lib/api";
import { bytes, clock, cx, mib, pct } from "../lib/format";
import { runningCells } from "../lib/store";
import { Button, Card, Empty, Spinner, Toggle } from "./ui";

const history: Record<string, Stats[]> = {};

export function MonitorTab({ session }: { session: string }) {
  const [samples, setSamples] = useState<Stats[]>(history[session] ?? []);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [auto, setAuto] = useState(false);
  const inflight = useRef(false);

  const refresh = useCallback(async () => {
    if (inflight.current) return;
    inflight.current = true;
    setLoading(true);
    setError(null);
    try {
      const s = await Colab.stats(session);
      const next = [...(history[session] ?? []), s].slice(-60);
      history[session] = next;
      setSamples(next);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      inflight.current = false;
      setLoading(false);
    }
  }, [session]);

  useEffect(() => {
    setSamples(history[session] ?? []);
    if (!(history[session]?.length)) void refresh();
  }, [session, refresh]);

  useEffect(() => {
    if (!auto) return;
    const t = setInterval(() => {
      // Measuring runs code in the kernel: skip while the user's code runs.
      if (runningCells(session).length === 0) void refresh();
    }, 15000);
    return () => clearInterval(t);
  }, [auto, session, refresh]);

  const s = samples[samples.length - 1];

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[980px] px-6 py-5">
        <div className="mb-4 flex items-center gap-3">
          <div>
            <div className="text-[13px] font-medium">资源监控</div>
            <div className="text-xs text-muted-foreground">
              通过在内核中执行一段采样代码获取（psutil + nvidia-smi）。内核忙碌时会等待。
            </div>
          </div>
          <div className="flex-1" />
          <Toggle checked={auto} onChange={setAuto} label="每 15 秒自动刷新" />
          <Button size="sm" loading={loading} icon={<RefreshCw className="size-3.5" />} onClick={refresh}>
            刷新
          </Button>
        </div>

        {error && <div className="mb-4 rounded-lg border border-danger/30 bg-danger/8 p-3 text-xs break-words whitespace-pre-wrap text-danger">{error}</div>}

        {!s && !error && (
          <div className="flex h-60 items-center justify-center gap-2 text-xs text-muted-foreground">
            <Spinner /> 正在采样…
          </div>
        )}

        {s && (
          <>
            <div className="grid grid-cols-3 gap-3">
              <Metric
                icon={<Cpu className="size-4" />}
                label="CPU"
                value={`${s.cpu.toFixed(0)}%`}
                sub={`${s.cpuCount} 核 · Python ${s.python}`}
                percent={s.cpu}
                series={samples.map((x) => x.cpu)}
              />
              <Metric
                icon={<MemoryStick className="size-4" />}
                label="系统内存"
                value={bytes(s.ramUsed)}
                sub={`共 ${bytes(s.ramTotal)}`}
                percent={pct(s.ramUsed, s.ramTotal)}
                series={samples.map((x) => pct(x.ramUsed, x.ramTotal))}
              />
              <Metric
                icon={<HardDrive className="size-4" />}
                label="磁盘 /content"
                value={bytes(s.diskUsed)}
                sub={`共 ${bytes(s.diskTotal)}`}
                percent={pct(s.diskUsed, s.diskTotal)}
                series={samples.map((x) => pct(x.diskUsed, x.diskTotal))}
              />
            </div>

            <div className="mt-5 mb-2 text-xs font-medium text-muted-foreground">加速器</div>
            {s.gpus.length === 0 ? (
              <Card className="p-6">
                <Empty icon={<Gauge className="size-5" />} title="没有检测到 NVIDIA GPU">
                  CPU 与 TPU 运行时没有 nvidia-smi。TPU 可在控制台运行 <code className="font-mono">import jax; jax.devices()</code> 查看。
                </Empty>
              </Card>
            ) : (
              <div className="grid grid-cols-2 gap-3">
                {s.gpus.map((g, i) => (
                  <Card key={i} className="p-4">
                    <div className="flex items-center justify-between">
                      <div className="text-[13px] font-semibold">{g.name}</div>
                      <div className="flex gap-3 text-[11px] text-muted-foreground">
                        <span className="flex items-center gap-1">
                          <Thermometer className="size-3" />
                          {g.temp.toFixed(0)}°C
                        </span>
                        <span className="flex items-center gap-1">
                          <Zap className="size-3" />
                          {g.power.toFixed(0)} W
                        </span>
                      </div>
                    </div>
                    <Bar label="显存" value={`${mib(g.memUsed)} / ${mib(g.memTotal)}`} percent={pct(g.memUsed, g.memTotal)} />
                    <Bar label="利用率" value={`${g.util.toFixed(0)}%`} percent={g.util} />
                    <Sparkline
                      className="mt-3"
                      series={[
                        samples.map((x) => pct(x.gpus[i]?.memUsed, x.gpus[i]?.memTotal)),
                        samples.map((x) => x.gpus[i]?.util ?? 0),
                      ]}
                    />
                  </Card>
                ))}
              </div>
            )}
            <div className="mt-4 flex items-center gap-1.5 text-[11px] text-muted-foreground">
              <Activity className="size-3" /> 最近采样 {clock(s.time)} · 共 {samples.length} 个样本
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function tone(p: number) {
  return p > 90 ? "bg-danger" : p > 70 ? "bg-warning" : "bg-accent";
}

function Metric({
  icon,
  label,
  value,
  sub,
  percent,
  series,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  sub: string;
  percent: number;
  series: number[];
}) {
  return (
    <Card className="p-4">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {icon}
        {label}
      </div>
      <div className="mt-1.5 text-lg font-semibold tabular-nums">{value}</div>
      <div className="text-[11px] text-muted-foreground">{sub}</div>
      <div className="mt-2.5 h-1.5 overflow-hidden rounded-full bg-muted">
        <div className={cx("h-full rounded-full transition-all", tone(percent))} style={{ width: `${percent}%` }} />
      </div>
      <Sparkline className="mt-3" series={[series]} />
    </Card>
  );
}

function Bar({ label, value, percent }: { label: string; value: string; percent: number }) {
  return (
    <div className="mt-3">
      <div className="mb-1 flex justify-between text-[11px]">
        <span className="text-muted-foreground">{label}</span>
        <span className="tabular-nums">{value}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div className={cx("h-full rounded-full transition-all", tone(percent))} style={{ width: `${percent}%` }} />
      </div>
    </div>
  );
}

function Sparkline({ series, className }: { series: number[][]; className?: string }) {
  const w = 300;
  const h = 36;
  const colors = ["var(--accent)", "var(--info)"];
  return (
    <svg viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" className={cx("h-9 w-full", className)}>
      <line x1="0" x2={w} y1={h - 0.5} y2={h - 0.5} stroke="var(--border)" />
      {series.map((data, k) => {
        if (data.length < 2) return null;
        const step = w / Math.max(1, data.length - 1);
        const pts = data.map((v, i) => `${(i * step).toFixed(1)},${(h - 2 - (Math.min(100, v) / 100) * (h - 4)).toFixed(1)}`);
        return <polyline key={k} points={pts.join(" ")} fill="none" stroke={colors[k]} strokeWidth="1.5" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />;
      })}
    </svg>
  );
}
