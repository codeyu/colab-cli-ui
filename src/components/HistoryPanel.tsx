import { useCallback, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, FileDown, History, Play, RefreshCw, ScrollText } from "lucide-react";
import { Colab, errorMessage, type HistoryEvent, type Session } from "../lib/api";
import { ReplayDialog } from "./ReplayDialog";
import { SaveScriptDialog } from "./StartupScripts";
import { needsDrive, replayable } from "../lib/startup";
import { cx, dateTime } from "../lib/format";
import { toast } from "../lib/store";
import { Badge, Button, Empty, IconButton, Spinner } from "./ui";

const TYPE_LABEL: Record<string, { label: string; tone: "neutral" | "accent" | "success" | "warning" | "danger" | "info" }> = {
  execution: { label: "执行", tone: "accent" },
  file_operation: { label: "文件", tone: "info" },
  automation: { label: "自动化", tone: "warning" },
  session_created: { label: "创建", tone: "success" },
  session_terminated: { label: "结束", tone: "danger" },
  stdin_request: { label: "输入", tone: "neutral" },
  input_reply: { label: "回复", tone: "neutral" },
};

export interface ReplayHost {
  targets: Session[];
  onGo: (target: string) => void;
  onSessionsChanged: () => Promise<void>;
}

export function HistoryPanel({ session, compact, host }: { session: string; compact?: boolean; host: ReplayHost }) {
  const [replaySteps, setReplaySteps] = useState<HistoryEvent[] | null>(null);
  const [saving, setSaving] = useState(false);
  const [events, setEvents] = useState<HistoryEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [filter, setFilter] = useState<string>("");
  const [open, setOpen] = useState<Set<number>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setEvents(await Colab.history(session, 500));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, [session]);

  useEffect(() => {
    setEvents(null);
    setOpen(new Set());
    void load();
  }, [load]);

  async function exportLog() {
    setExporting(true);
    try {
      const p = await Colab.exportHistory(session);
      if (p) toast("success", "已导出会话历史", p);
    } catch (e) {
      toast("error", "导出失败", errorMessage(e));
    } finally {
      setExporting(false);
    }
  }

  const steps = (events ?? []).filter(replayable);
  const created = events?.find((e) => e.type === "session_created");
  const origin = { accelerator: created?.fields?.accelerator ?? "NONE", highMem: created?.fields?.machine_shape === "HIGH_RAM" };

  const shown = (events ?? []).map((e, i) => ({ e, i })).filter(({ e }) => !filter || e.type === filter).reverse();
  const types = [...new Set((events ?? []).map((e) => e.type))];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className={cx("flex items-center gap-2 border-b border-border py-2", compact ? "px-4" : "px-6")}>
        <div className="flex flex-wrap gap-1">
          <FilterChip active={!filter} onClick={() => setFilter("")}>
            全部 {events ? events.length : ""}
          </FilterChip>
          {types.map((t) => (
            <FilterChip key={t} active={filter === t} onClick={() => setFilter(t)}>
              {TYPE_LABEL[t]?.label ?? t}
            </FilterChip>
          ))}
        </div>
        <div className="flex-1" />
        <IconButton label="刷新" onClick={load} disabled={loading}>
          {loading ? <Spinner /> : <RefreshCw className="size-3.5" />}
        </IconButton>
        <Button size="sm" icon={<ScrollText className="size-3.5" />} onClick={() => setSaving(true)} disabled={steps.length === 0}>
          保存为启动脚本
        </Button>
        <Button size="sm" loading={exporting} icon={<FileDown className="size-3.5" />} onClick={exportLog} disabled={!events?.length}>
          导出（ipynb / md / txt / jsonl）
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {error && <div className="m-4 text-xs text-danger">{error}</div>}
        {events === null && !error && (
          <div className="flex items-center gap-2 p-4 text-xs text-muted-foreground">
            <Spinner /> 读取历史…
          </div>
        )}
        {events?.length === 0 && (
          <Empty icon={<History className="size-5" />} title="暂无历史">
            CLI 会把执行、文件操作等事件记录在 ~/.config/colab-cli/history。
          </Empty>
        )}
        <div className={cx("flex flex-col py-2", compact ? "px-2" : "px-4")}>
          {shown.map(({ e, i }) => {
            const t = TYPE_LABEL[e.type] ?? { label: e.type, tone: "neutral" as const };
            const expandable = !!(e.code || e.output);
            const isOpen = open.has(i);
            return (
              <div key={i} className="group rounded-lg hover:bg-muted/50">
                <button
                  className="flex w-full items-center gap-2.5 px-2 py-1.5 text-left"
                  onClick={() =>
                    expandable &&
                    setOpen((s) => {
                      const n = new Set(s);
                      if (n.has(i)) n.delete(i);
                      else n.add(i);
                      return n;
                    })
                  }
                >
                  <span className="w-3 text-muted-foreground">
                    {expandable && (isOpen ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />)}
                  </span>
                  <span className="w-36 shrink-0 text-[11px] text-muted-foreground tabular-nums">{dateTime(e.timestamp)}</span>
                  <Badge tone={t.tone} className="w-14 justify-center">
                    {t.label}
                  </Badge>
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">{e.summary}</span>
                  {replayable(e) && (
                    <span
                      role="button"
                      title="重新执行"
                      onClick={(ev) => {
                        ev.stopPropagation();
                        setReplaySteps([e]);
                      }}
                      className="rounded p-1 text-muted-foreground opacity-0 group-hover:opacity-100 hover:bg-muted hover:text-foreground"
                    >
                      <Play className="size-3" />
                    </span>
                  )}
                </button>
                {isOpen && (
                  <div className="mx-2 mb-2 ml-9 overflow-hidden rounded-lg border border-border">
                    {e.code && <pre className="selectable max-h-72 overflow-auto bg-muted/50 px-3 py-2 font-mono text-xs">{e.code}</pre>}
                    {e.output && <pre className="selectable max-h-72 overflow-auto border-t border-border bg-console px-3 py-2 font-mono text-xs">{e.output}</pre>}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
      <SaveScriptDialog open={saving} onClose={() => setSaving(false)} source={session} steps={steps} drive={needsDrive(events ?? [])} />
      <ReplayDialog
        open={replaySteps !== null}
        onClose={() => setReplaySteps(null)}
        source={session}
        steps={replaySteps ?? []}
        drive={needsDrive(events ?? [])}
        origin={origin}
        targets={host.targets}
        onGo={host.onGo}
        onSessionsChanged={host.onSessionsChanged}
      />
    </div>
  );
}

function FilterChip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={cx(
        "h-6 rounded-md px-2 text-[11px] font-medium transition-colors",
        active ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

export function HistoryView({ host }: { host: ReplayHost }) {
  const [names, setNames] = useState<string[] | null>(null);
  const [sel, setSel] = useState<string | null>(null);
  useEffect(() => {
    Colab.historySessions()
      .then((n) => {
        setNames(n);
        setSel((s) => s ?? n[0] ?? null);
      })
      .catch((e) => toast("error", "读取历史失败", errorMessage(e)));
  }, []);
  return (
    <div className="flex h-full min-h-0">
      <div className="w-52 shrink-0 overflow-y-auto border-r border-border p-2">
        <div className="px-2 pt-1 pb-2 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">有记录的会话</div>
        {names === null && <Spinner className="m-2" />}
        {names?.length === 0 && <div className="px-2 text-xs text-muted-foreground">暂无</div>}
        {names?.map((n) => (
          <button
            key={n}
            onClick={() => setSel(n)}
            className={cx("block w-full truncate rounded-lg px-2 py-1.5 text-left text-[13px]", sel === n ? "bg-muted font-medium" : "hover:bg-muted/60")}
          >
            {n}
          </button>
        ))}
      </div>
      <div className="min-w-0 flex-1">{sel ? <HistoryPanel session={sel} compact host={host} /> : <Empty icon={<History className="size-5" />} title="选择一个会话" />}</div>
    </div>
  );
}
