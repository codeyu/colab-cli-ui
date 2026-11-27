import { useEffect, useRef, useState } from "react";
import {
  Activity,
  Boxes,
  Bot,
  Cable,
  ChevronDown,
  Cloud,
  ExternalLink,
  FolderOpen,
  HardDriveDownload,
  History,
  KeyRound,
  Package,
  Power,
  RotateCw,
  SquareTerminal,
  Terminal,
  TerminalSquare,
} from "lucide-react";
import { Colab, errorMessage, type Session } from "../lib/api";
import { acceleratorInfo, spentLine } from "../lib/accelerators";
import { cx, useNow } from "../lib/format";
import { cellsStore, toast, useStore } from "../lib/store";
import { ConsoleTab, setConsoleDraft } from "./ConsoleTab";
import { GuardChip } from "./GuardChip";
import { ModelsTab } from "./ModelsTab";
import { FilesTab } from "./FilesTab";
import { HistoryPanel } from "./HistoryPanel";
import { MonitorTab } from "./MonitorTab";
import { PackagesTab } from "./PackagesTab";
import { PortsTab } from "./PortsTab";
import { ApiTab } from "./ApiTab";
import { useForwards } from "../lib/forwards";
import { Badge, Button, Dialog, StatusDot } from "./ui";

type Tab = "console" | "models" | "api" | "files" | "ports" | "packages" | "monitor" | "history";
const lastTab: Record<string, Tab> = {};

const TABS: { id: Tab; label: string; icon: React.ReactNode }[] = [
  { id: "console", label: "控制台", icon: <Terminal className="size-3.5" /> },
  { id: "models", label: "模型", icon: <Boxes className="size-3.5" /> },
  { id: "api", label: "模型 API", icon: <Bot className="size-3.5" /> },
  { id: "files", label: "文件", icon: <FolderOpen className="size-3.5" /> },
  { id: "ports", label: "端口", icon: <Cable className="size-3.5" /> },
  { id: "packages", label: "包管理", icon: <Package className="size-3.5" /> },
  { id: "monitor", label: "资源监控", icon: <Activity className="size-3.5" /> },
  { id: "history", label: "历史", icon: <History className="size-3.5" /> },
];

export function SessionView({
  session,
  onStopped,
  onChanged,
  sessions,
  onGo,
}: {
  session: Session;
  onStopped: () => void;
  onChanged: () => void | Promise<void>;
  sessions: Session[];
  onGo: (name: string) => void;
}) {
  const name = session.name;
  const [tab, setTab] = useState<Tab>(lastTab[name] ?? "console");
  const [confirm, setConfirm] = useState<"stop" | "restart" | null>(null);
  const [acting, setActing] = useState<string | null>(null);
  const cells = useStore(cellsStore)[name] ?? [];
  const running = cells.some((c) => c.status === "running");
  const info = acceleratorInfo(session.hardware);
  const now = useNow();
  const forwards = useForwards(name);
  const spent = spentLine(session, now);

  useEffect(() => setTab(lastTab[name] ?? "console"), [name]);

  async function act(kind: string, fn: () => Promise<unknown>, ok?: string) {
    setActing(kind);
    try {
      await fn();
      if (ok) toast("success", ok);
    } catch (e) {
      toast("error", "操作失败", errorMessage(e));
    } finally {
      setActing(null);
    }
  }

  async function stop() {
    setConfirm(null);
    for (const c of cells) c.abort?.abort();
    await act("stop", () => Colab.stopSession(name), `会话 ${name} 已释放`);
    onStopped();
  }

  async function restart() {
    setConfirm(null);
    await act("restart", () => Colab.restartKernel(name), "内核已重启，变量已清空");
    onChanged();
  }

  const terminal = (kind: string, label: string) =>
    act(kind, () => Colab.openTerminal(name, kind), `已在新终端窗口打开 ${label}`);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-border px-6 pt-4">
        <div className="flex items-start gap-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2.5">
              <StatusDot tone={session.busy || running ? "warning" : "success"} pulse={session.busy || running} />
              <h1 className="truncate text-[17px] font-semibold">{name}</h1>
              <Badge tone={session.hardware === "CPU" ? "neutral" : "accent"}>
                {info?.label ?? session.hardware}
                {info && info.kind !== "CPU" ? ` · ${info.memory}` : ""}
              </Badge>
              {session.shape && <Badge>{session.shape}</Badge>}
              <Badge tone={session.busy || running ? "warning" : "success"}>{session.busy || running ? "运行中" : session.status || "IDLE"}</Badge>
              <GuardChip session={name} />
            </div>
            <div className="mt-1 flex gap-3 truncate text-[11px] text-muted-foreground">
              <span className="font-mono" title="endpoint">
                {session.endpoint}
              </span>
              {spent && <span title="按开机时间和估算费率计算">{spent}</span>}
              {session.lastExecution && <span>上次执行：{session.lastExecution}</span>}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <Button size="sm" icon={<ExternalLink className="size-3.5" />} loading={acting === "browser"} onClick={() => act("browser", () => Colab.openInBrowser(name))}>
              在 Colab 中打开
            </Button>
            <Menu
              label="终端"
              icon={<TerminalSquare className="size-3.5" />}
              items={[
                { label: "Python REPL", hint: "colab repl", icon: <SquareTerminal className="size-3.5" />, onClick: () => terminal("repl", "REPL") },
                { label: "SSH Shell", hint: "colab ssh", icon: <Terminal className="size-3.5" />, onClick: () => terminal("ssh", "SSH") },
                { label: "挂载 Google Drive", hint: "colab drivemount", icon: <HardDriveDownload className="size-3.5" />, onClick: () => terminal("drivemount", "Drive 挂载") },
                { label: "VM 端 GCP 授权", hint: "colab auth", icon: <KeyRound className="size-3.5" />, onClick: () => terminal("auth", "GCP 授权") },
              ]}
            />
            <Button size="sm" icon={<RotateCw className="size-3.5" />} loading={acting === "restart"} onClick={() => setConfirm("restart")}>
              重启内核
            </Button>
            <Button size="sm" variant="danger" icon={<Power className="size-3.5" />} loading={acting === "stop"} onClick={() => setConfirm("stop")}>
              释放
            </Button>
          </div>
        </div>
        <nav className="mt-3 flex gap-1">
          {TABS.map((t) => (
            <button
              key={t.id}
              onClick={() => {
                setTab(t.id);
                lastTab[name] = t.id;
              }}
              className={cx(
                "-mb-px flex h-8 items-center gap-1.5 border-b-2 px-2.5 text-[13px] transition-colors",
                tab === t.id ? "border-accent font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {t.icon}
              {t.label}
              {t.id === "console" && running && <span className="size-1.5 rounded-full bg-accent pulse-dot" />}
              {t.id === "ports" && forwards.length > 0 && <span className="text-[11px] text-muted-foreground">{forwards.length}</span>}
            </button>
          ))}
        </nav>
      </header>

      <div className="min-h-0 flex-1">
        {tab === "console" && <ConsoleTab session={name} />}
        {tab === "models" && (
          <ModelsTab
            session={session}
            onUseInConsole={(code) => {
              setConsoleDraft(name, code);
              setTab("console");
              lastTab[name] = "console";
            }}
          />
        )}
        {tab === "api" && <ApiTab session={session} />}
        {tab === "files" && <FilesTab session={name} />}
        {tab === "ports" && <PortsTab session={name} />}
        {tab === "packages" && <PackagesTab session={name} />}
        {tab === "monitor" && <MonitorTab session={name} />}
        {tab === "history" && (
          <HistoryPanel
            session={name}
            host={{
              targets: sessions,
              onGo: (target) => {
                if (target === name) {
                  setTab("console");
                  lastTab[name] = "console";
                } else onGo(target);
              },
              onSessionsChanged: async () => {
                await onChanged();
              },
            }}
          />
        )}
      </div>

      <Dialog
        open={confirm === "stop"}
        onClose={() => setConfirm(null)}
        title={`释放会话 ${name}？`}
        description="虚拟机会被关闭并归还（colab stop），内核状态和 /content 下未保存的文件都会丢失。"
        width={440}
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirm(null)}>
              取消
            </Button>
            <Button variant="danger" onClick={stop} icon={<Power className="size-3.5" />}>
              释放虚拟机
            </Button>
          </>
        }
      >
        {running ? <div className="text-xs text-warning">有任务正在运行，释放会中止它们。</div> : <div />}
      </Dialog>
      <Dialog
        open={confirm === "restart"}
        onClose={() => setConfirm(null)}
        title="重启内核？"
        description="虚拟机保留，但 Python 内核的变量、导入和加载的模型都会被清空。"
        width={420}
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirm(null)}>
              取消
            </Button>
            <Button variant="primary" onClick={restart}>
              重启
            </Button>
          </>
        }
      >
        <div />
      </Dialog>
    </div>
  );
}

function Menu({
  label,
  icon,
  items,
}: {
  label: string;
  icon: React.ReactNode;
  items: { label: string; hint?: string; icon: React.ReactNode; onClick: () => void }[];
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    window.addEventListener("mousedown", close);
    return () => window.removeEventListener("mousedown", close);
  }, [open]);
  return (
    <div ref={ref} className="relative">
      <Button size="sm" icon={icon} onClick={() => setOpen((o) => !o)}>
        {label}
        <ChevronDown className="size-3 text-muted-foreground" />
      </Button>
      {open && (
        <div className="fade-in absolute right-0 z-40 mt-1 w-60 rounded-xl border border-border bg-popover p-1 shadow-xl">
          {items.map((it) => (
            <button
              key={it.label}
              onClick={() => {
                setOpen(false);
                it.onClick();
              }}
              className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[13px] hover:bg-muted"
            >
              <span className="text-muted-foreground">{it.icon}</span>
              <span className="flex-1">{it.label}</span>
              {it.hint && <span className="font-mono text-[10px] text-muted-foreground">{it.hint}</span>}
            </button>
          ))}
          <div className="mt-1 border-t border-border px-2.5 pt-1.5 pb-1 text-[11px] leading-snug text-muted-foreground">
            <Cloud className="mr-1 inline size-3" />
            这些命令需要交互式终端，会在新的终端窗口中打开。
          </div>
        </div>
      )}
    </div>
  );
}
