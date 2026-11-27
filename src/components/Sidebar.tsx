import {
  Library,
  Boxes,
  Coins,
  History,
  Moon,
  Plus,
  RefreshCw,
  Rocket,
  Settings as SettingsIcon,
  Sun,
  SunMoon,
  TerminalSquare,
} from "lucide-react";
import type { Account, Session, Usage } from "../lib/api";
import { AccountMenu } from "./AccountMenu";
import { cx, timeAgo } from "../lib/format";
import { cellsStore, useStore } from "../lib/store";
import { Badge, IconButton, Spinner, StatusDot } from "./ui";
import { AppMark } from "./AppMark";

export type View = { kind: "home" } | { kind: "session"; name: string } | { kind: "models" } | { kind: "tasks" } | { kind: "history" } | { kind: "settings" };
export type Theme = "system" | "light" | "dark";

export function Sidebar({
  view,
  setView,
  sessions,
  sessionsLoading,
  sessionsError,
  onRefresh,
  onNew,
  usage,
  usageLoading,
  onRefreshUsage,
  account,
  onAccountChanged,
  version,
  theme,
  setTheme,
  activityOpen,
  toggleActivity,
  runningCount,
}: {
  view: View;
  setView: (v: View) => void;
  sessions: Session[] | null;
  sessionsLoading: boolean;
  sessionsError: string | null;
  onRefresh: () => void;
  onNew: () => void;
  usage: Usage | null;
  usageLoading: boolean;
  onRefreshUsage: () => void;
  account: Account | null;
  onAccountChanged: () => void;
  version: string;
  theme: Theme;
  setTheme: (t: Theme) => void;
  activityOpen: boolean;
  toggleActivity: () => void;
  runningCount: number;
}) {
  const cells = useStore(cellsStore);
  const nextTheme: Record<Theme, Theme> = { system: "light", light: "dark", dark: "system" };

  return (
    <aside className="flex w-[260px] shrink-0 flex-col border-r border-border bg-sidebar">
      <div className="flex items-center gap-2.5 px-4 pt-4 pb-3">
        <AppMark size={28} />
        <div className="min-w-0 leading-tight">
          <div className="text-[13px] font-semibold">Colab CLI</div>
          <div className="text-[11px] text-muted-foreground">{version ? `google-colab-cli ${version}` : "检测中…"}</div>
        </div>
      </div>

      <div className="px-2.5">
        <button
          onClick={onNew}
          className="flex h-8 w-full items-center gap-2 rounded-lg border border-border bg-card px-2.5 text-[13px] font-medium shadow-xs transition-colors hover:bg-muted"
        >
          <Plus className="size-4 text-accent" />
          新建会话
          <span className="ml-auto text-[11px] text-muted-foreground">Ctrl N</span>
        </button>
      </div>

      <nav className="mt-3 flex flex-col gap-0.5 px-2.5">
        <NavItem icon={<Rocket className="size-4" />} active={view.kind === "home"} onClick={() => setView({ kind: "home" })}>
          概览与一次性任务
        </NavItem>
        <NavItem icon={<Boxes className="size-4" />} active={view.kind === "models"} onClick={() => setView({ kind: "models" })}>
          模型
        </NavItem>
        <NavItem icon={<Library className="size-4" />} active={view.kind === "tasks"} onClick={() => setView({ kind: "tasks" })}>
          任务库
        </NavItem>
        <NavItem icon={<History className="size-4" />} active={view.kind === "history"} onClick={() => setView({ kind: "history" })}>
          历史记录
        </NavItem>
      </nav>

      <div className="mt-4 flex items-center justify-between px-4 pb-1">
        <span className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">会话</span>
        <IconButton label="刷新会话" onClick={onRefresh} disabled={sessionsLoading} className="size-6">
          {sessionsLoading ? <Spinner className="size-3" /> : <RefreshCw className="size-3.5" />}
        </IconButton>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2.5 pb-2">
        {sessions === null && !sessionsError && (
          <div className="flex items-center gap-2 px-2 py-2 text-xs text-muted-foreground">
            <Spinner /> 正在读取会话…
          </div>
        )}
        {sessionsError && <div className="px-2 py-2 text-xs break-words text-danger">{sessionsError}</div>}
        {sessions?.length === 0 && (
          <div className="px-2 py-2 text-xs leading-relaxed text-muted-foreground">
            没有运行中的会话。点击「新建会话」申请一台 Colab 虚拟机。
          </div>
        )}
        {sessions?.map((s) => {
          const running = (cells[s.name] ?? []).some((c) => c.status === "running");
          const active = view.kind === "session" && view.name === s.name;
          return (
            <button
              key={s.endpoint}
              onClick={() => s.local && setView({ kind: "session", name: s.name })}
              title={s.local ? s.endpoint : "这台虚拟机不是由本机创建的，CLI 没有它的本地记录"}
              className={cx(
                "group flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors",
                active ? "bg-sidebar-accent" : "hover:bg-sidebar-accent/70",
                !s.local && "opacity-60",
              )}
            >
              <StatusDot
                tone={s.busy || running ? "warning" : s.local ? "success" : "muted"}
                pulse={s.busy || running}
              />
              <div className="min-w-0 flex-1">
                <div className="truncate text-[13px] font-medium">{s.name}</div>
                <div className="truncate text-[11px] text-muted-foreground">
                  {s.busy || running ? "运行中" : s.status || (s.local ? "就绪" : "未在本机登记")}
                  {s.shape && s.shape !== "Standard" ? ` · ${s.shape}` : ""}
                </div>
              </div>
              <Badge tone={s.hardware === "CPU" ? "neutral" : "accent"}>{s.hardware}</Badge>
            </button>
          );
        })}
      </div>

      <div className="border-t border-border p-2.5">
        <div className="rounded-xl border border-border bg-card p-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
              <Coins className="size-3.5 text-accent" /> 计算单元
            </div>
            <IconButton label="刷新用量" onClick={onRefreshUsage} disabled={usageLoading} className="size-6">
              {usageLoading ? <Spinner className="size-3" /> : <RefreshCw className="size-3" />}
            </IconButton>
          </div>
          <div className="mt-1 flex items-baseline gap-1.5">
            <span className="text-xl font-semibold tabular-nums">{usage ? usage.balance.toFixed(2) : "—"}</span>
            <span className="text-[11px] text-muted-foreground">CU</span>
          </div>
          <div className="mt-0.5 flex justify-between text-[11px] text-muted-foreground">
            <span>
              消耗 <span className={cx("tabular-nums", usage && usage.ratePerHour > 0 && "text-warning")}>{usage ? usage.ratePerHour.toFixed(2) : "—"}</span>/小时
            </span>
            <span>{usage ? timeAgo(usage.fetchedAt) : ""}</span>
          </div>
        </div>
        <div className="mt-2 flex items-center gap-1">
          <AccountMenu email={account?.email ?? ""} running={sessions?.filter((s) => s.local).length ?? 0} onChanged={onAccountChanged} />
          <IconButton label="CLI 命令日志" active={activityOpen} onClick={toggleActivity} className="relative">
            <TerminalSquare className="size-4" />
            {runningCount > 0 && <span className="absolute top-1 right-1 size-1.5 rounded-full bg-accent" />}
          </IconButton>
          <IconButton
            label={theme === "system" ? "主题：跟随系统" : theme === "light" ? "主题：浅色" : "主题：深色"}
            onClick={() => setTheme(nextTheme[theme])}
          >
            {theme === "system" ? <SunMoon className="size-4" /> : theme === "light" ? <Sun className="size-4" /> : <Moon className="size-4" />}
          </IconButton>
          <IconButton label="设置" active={view.kind === "settings"} onClick={() => setView({ kind: "settings" })}>
            <SettingsIcon className="size-4" />
          </IconButton>
        </div>
      </div>
    </aside>
  );
}

function NavItem({
  icon,
  active,
  onClick,
  children,
}: {
  icon: React.ReactNode;
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={cx(
        "flex h-8 items-center gap-2 rounded-lg px-2 text-[13px] transition-colors",
        active ? "bg-sidebar-accent font-medium text-foreground" : "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground",
      )}
    >
      {icon}
      {children}
    </button>
  );
}
