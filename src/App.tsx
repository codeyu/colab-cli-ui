import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, Settings as SettingsIcon } from "lucide-react";
import { Colab, errorMessage, events, type Account, type Environment, type Session, type Usage } from "./lib/api";
import { activityStore, recordActivity, toast, useStore } from "./lib/store";
import { ActivityDrawer } from "./components/ActivityDrawer";
import { HistoryView } from "./components/HistoryPanel";
import { HomeView } from "./components/HomeView";
import { ModelsTab } from "./components/ModelsTab";
import { NewSessionDialog } from "./components/NewSessionDialog";
import { SessionView } from "./components/SessionView";
import { SettingsView } from "./components/SettingsView";
import { Sidebar, type Theme, type View } from "./components/Sidebar";
import { Button, Empty, Spinner, Toasts } from "./components/ui";

function readTheme(): Theme {
  try {
    const t = localStorage.getItem("theme");
    if (t === "light" || t === "dark" || t === "system") return t;
  } catch {
    /* storage unavailable */
  }
  return "system";
}

function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setTheme] = useState<Theme>(readTheme);
  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => document.documentElement.classList.toggle("dark", theme === "dark" || (theme === "system" && mq.matches));
    apply();
    mq.addEventListener("change", apply);
    try {
      localStorage.setItem("theme", theme);
    } catch {
      /* storage unavailable */
    }
    return () => mq.removeEventListener("change", apply);
  }, [theme]);
  return [theme, setTheme];
}

export function App() {
  const [theme, setTheme] = useTheme();
  const [view, setView] = useState<View>({ kind: "home" });
  const [env, setEnv] = useState<Environment | null>(null);
  const [account, setAccount] = useState<Account | null>(null);
  const [accountError, setAccountError] = useState<string | null>(null);
  const [sessions, setSessions] = useState<Session[] | null>(null);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsError, setSessionsError] = useState<string | null>(null);
  const [usage, setUsage] = useState<Usage | null>(null);
  const [usageLoading, setUsageLoading] = useState(false);
  const [newOpen, setNewOpen] = useState(false);
  const [activityOpen, setActivityOpen] = useState(false);
  const [refreshSeconds, setRefreshSeconds] = useState(30);
  const activity = useStore(activityStore);

  // A refresh asked for while one runs joins it. After a change (a session
  // created or stopped) pass fresh, which waits for it and loads again, since
  // the running one may predate the change.
  const inflight = useRef<Promise<void> | null>(null);
  const loadSessions = useCallback((fresh = false): Promise<void> => {
    if (inflight.current) return fresh ? inflight.current.then(() => loadSessions()) : inflight.current;
    setSessionsLoading(true);
    const p = (async () => {
      try {
        setSessions(await Colab.sessions());
        setSessionsError(null);
      } catch (e) {
        setSessionsError(errorMessage(e));
      } finally {
        inflight.current = null;
        setSessionsLoading(false);
      }
    })();
    inflight.current = p;
    return p;
  }, []);

  const loadUsage = useCallback(async () => {
    setUsageLoading(true);
    try {
      setUsage(await Colab.usage());
    } catch (e) {
      toast("error", "读取计算单元失败", errorMessage(e));
    } finally {
      setUsageLoading(false);
    }
  }, []);

  const loadAccount = useCallback(async () => {
    setAccount(null);
    setAccountError(null);
    try {
      setAccount(await Colab.whoami());
    } catch (e) {
      setAccountError(errorMessage(e));
    }
  }, []);

  const boot = useCallback(async () => {
    setEnv(null);
    const [e, s] = await Promise.all([Colab.environment(), Colab.getSettings()]);
    setEnv(e);
    setRefreshSeconds(s.refreshSeconds);
    if (e.ok) {
      void loadSessions();
      void loadUsage();
      void loadAccount();
    }
  }, [loadSessions, loadUsage, loadAccount]);

  useEffect(() => {
    const off = events.activity.on(recordActivity);
    void boot();
    return off;
  }, [boot]);

  useEffect(() => {
    if (!env?.ok || refreshSeconds <= 0) return;
    const t = setInterval(() => {
      if (document.visibilityState === "visible") void loadSessions();
    }, refreshSeconds * 1000);
    const u = setInterval(() => document.visibilityState === "visible" && void loadUsage(), Math.max(60, refreshSeconds * 2) * 1000);
    return () => {
      clearInterval(t);
      clearInterval(u);
    };
  }, [env, refreshSeconds, loadSessions, loadUsage]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "n" && env?.ok) {
        e.preventDefault();
        setNewOpen(true);
      }
      if ((e.ctrlKey || e.metaKey) && e.key === "`") {
        e.preventDefault();
        setActivityOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [env]);

  const current = view.kind === "session" ? sessions?.find((s) => s.name === view.name) : undefined;
  const runningCount = activity.filter((a) => a.running).length;

  let main: React.ReactNode;
  if (env && !env.ok && view.kind !== "settings") {
    main = (
      <Empty icon={<AlertTriangle className="size-5 text-warning" />} title="没有可用的 Colab CLI">
        <div className="text-danger">{env.error}</div>
        <pre className="selectable mt-3 rounded-lg bg-muted px-3 py-2 text-left font-mono text-xs text-foreground">uv tool install google-colab-cli</pre>
        <div className="mt-4 flex justify-center gap-2">
          <Button size="sm" onClick={boot}>
            重新检测
          </Button>
          <Button size="sm" icon={<SettingsIcon className="size-3.5" />} onClick={() => setView({ kind: "settings" })}>
            指定路径
          </Button>
        </div>
      </Empty>
    );
  } else if (view.kind === "settings") {
    main = <SettingsView env={env} account={account} accountError={accountError} onSaved={boot} onCheckAccount={loadAccount} />;
  } else if (view.kind === "models") {
    main = <ModelsTab session={null} />;
  } else if (view.kind === "history") {
    main = (
      <HistoryView
        host={{
          targets: sessions ?? [],
          onGo: (name) => setView({ kind: "session", name }),
          onSessionsChanged: () => loadSessions(true),
        }}
      />
    );
  } else if (view.kind === "session") {
    main = current ? (
      <SessionView
        key={current.name}
        session={current}
        onStopped={() => {
          setView({ kind: "home" });
          void loadSessions();
          void loadUsage();
        }}
        onChanged={() => loadSessions(true)}
        sessions={sessions ?? []}
        onGo={(name) => setView({ kind: "session", name })}
      />
    ) : sessions === null || sessionsLoading ? (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    ) : (
      <Empty icon={<AlertTriangle className="size-5" />} title={`会话 ${view.name} 已不存在`}>
        它可能已被释放，或者被 Colab 后端回收。
        <div className="mt-3">
          <Button size="sm" onClick={() => setView({ kind: "home" })}>
            返回概览
          </Button>
        </div>
      </Empty>
    );
  } else {
    main = (
      <HomeView
        sessions={sessions}
        usage={usage}
        account={account}
        onOpen={(name) => setView({ kind: "session", name })}
        onNew={() => setNewOpen(true)}
        onChanged={() => {
          void loadSessions();
          void loadUsage();
        }}
      />
    );
  }

  return (
    <div className="flex h-full">
      <Sidebar
        view={view}
        setView={setView}
        sessions={sessions}
        sessionsLoading={sessionsLoading}
        sessionsError={sessionsError}
        onRefresh={() => loadSessions()}
        onNew={() => setNewOpen(true)}
        usage={usage}
        usageLoading={usageLoading}
        onRefreshUsage={loadUsage}
        account={account}
        version={env?.version ?? ""}
        theme={theme}
        setTheme={setTheme}
        activityOpen={activityOpen}
        toggleActivity={() => setActivityOpen((o) => !o)}
        runningCount={runningCount}
      />
      <main className="flex min-w-0 flex-1 flex-col">
        <div className="min-h-0 flex-1">{main}</div>
        {activityOpen && <ActivityDrawer onClose={() => setActivityOpen(false)} />}
      </main>
      <NewSessionDialog
        open={newOpen}
        usage={usage}
        onClose={() => setNewOpen(false)}
        onCreated={async (name) => {
          await loadSessions(true);
          void loadUsage();
          setView({ kind: "session", name });
        }}
      />
      <Toasts />
    </div>
  );
}
