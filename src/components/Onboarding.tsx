import { useState } from "react";
import { Check, Copy, ExternalLink, LogIn, RefreshCw } from "lucide-react";
import { Colab, errorMessage, streamCall, type AuthState, type Chunk, type SetupStatus } from "../lib/api";
import { cx } from "../lib/format";
import { toast } from "../lib/store";
import { Output } from "./Output";
import { Button, Spinner, inputClass } from "./ui";
import { AppMark } from "./AppMark";

const WINDOWS = navigator.userAgent.includes("Windows");
const UV_INSTALL = WINDOWS
  ? `powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"`
  : "curl -LsSf https://astral.sh/uv/install.sh | sh";
const ADC_LOGIN =
  "gcloud auth application-default login --scopes=openid,https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/userinfo.email,https://www.googleapis.com/auth/colaboratory";

/** First run: install uv and the CLI, then sign in to Google. */
export function Onboarding({
  setup,
  auth,
  onRecheck,
}: {
  setup: SetupStatus;
  auth: AuthState | null;
  onRecheck: () => Promise<void>;
}) {
  const uvOK = !!setup.uvPath;
  const cliOK = setup.env.ok;
  const loggedIn = !!auth?.loggedIn;

  return (
    <div className="flex h-full items-center justify-center overflow-y-auto p-6">
      <div className="w-full max-w-[560px]">
        <div className="mb-6 flex items-center gap-3">
          <AppMark size={40} />
          <div>
            <div className="text-lg font-semibold">开始使用 Colab CLI</div>
            <div className="text-xs text-muted-foreground">三步完成，之后不用再做</div>
          </div>
        </div>

        <div className="flex flex-col gap-3">
          <Step n={1} title="安装 uv" done={uvOK || cliOK} active={!uvOK && !cliOK} detail={uvOK ? setup.uvPath : cliOK ? "已跳过" : undefined}>
            <UVStep onRecheck={onRecheck} />
          </Step>
          <Step n={2} title="安装 Colab CLI" done={cliOK} active={uvOK && !cliOK} detail={cliOK ? `google-colab-cli ${setup.env.version}` : undefined}>
            <CLIStep onRecheck={onRecheck} />
          </Step>
          <Step n={3} title="登录 Google 账号" done={loggedIn} active={cliOK && !loggedIn} detail={loggedIn ? auth?.account.email : undefined}>
            <LoginStep auth={auth} onRecheck={onRecheck} />
          </Step>
        </div>
      </div>
    </div>
  );
}

function Step({
  n,
  title,
  done,
  active,
  detail,
  children,
}: {
  n: number;
  title: string;
  done: boolean;
  active: boolean;
  detail?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={cx("rounded-xl border bg-card p-4 transition-colors", active ? "border-accent/50" : "border-border", !active && !done && "opacity-50")}>
      <div className="flex items-center gap-3">
        <span
          className={cx(
            "flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
            done ? "bg-success text-white" : active ? "bg-accent text-white" : "bg-muted text-muted-foreground",
          )}
        >
          {done ? <Check className="size-3.5" /> : n}
        </span>
        <span className="shrink-0 text-[13px] font-medium whitespace-nowrap">{title}</span>
        {detail && (
          <span className="ml-auto min-w-0 truncate font-mono text-[11px] text-muted-foreground" title={detail}>
            {detail}
          </span>
        )}
      </div>
      {active && <div className="mt-3 pl-9">{children}</div>}
    </div>
  );
}

function CopyLine({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-start gap-2 rounded-lg bg-muted px-3 py-2">
      <code className="selectable min-w-0 flex-1 font-mono text-[11px] leading-relaxed break-all">{text}</code>
      <button
        className="mt-0.5 text-muted-foreground hover:text-foreground"
        title="复制"
        onClick={() => {
          void navigator.clipboard.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        }}
      >
        {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  );
}

function Recheck({ onRecheck, label = "已完成，重新检测" }: { onRecheck: () => Promise<void>; label?: string }) {
  const [busy, setBusy] = useState(false);
  return (
    <Button
      size="sm"
      loading={busy}
      icon={<RefreshCw className="size-3.5" />}
      onClick={async () => {
        setBusy(true);
        await onRecheck();
        setBusy(false);
      }}
    >
      {label}
    </Button>
  );
}

function UVStep({ onRecheck }: { onRecheck: () => Promise<void> }) {
  return (
    <div className="flex flex-col gap-2.5">
      <div className="text-xs text-muted-foreground">Colab CLI 用 uv 安装。在{WINDOWS ? " PowerShell " : "终端"}里运行：</div>
      <CopyLine text={UV_INSTALL} />
      <div className="flex gap-2">
        <Recheck onRecheck={onRecheck} />
        <Button size="sm" variant="ghost" icon={<ExternalLink className="size-3.5" />} onClick={() => Colab.openExternal("https://docs.astral.sh/uv/getting-started/installation/")}>
          uv 安装说明
        </Button>
      </div>
    </div>
  );
}

function CLIStep({ onRecheck }: { onRecheck: () => Promise<void> }) {
  const [chunks, setChunks] = useState<Chunk[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function install() {
    setBusy(true);
    setError(null);
    setChunks([]);
    try {
      await streamCall((ch) => Colab.installCLI(ch), (c) => setChunks((l) => [...l, c]));
      await onRecheck();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function pick() {
    const p = await Colab.pickExecutable();
    if (!p) return;
    try {
      const s = await Colab.getSettings();
      await Colab.saveSettings({ ...s, colabPath: p });
      await onRecheck();
    } catch (e) {
      toast("error", "保存失败", errorMessage(e));
    }
  }

  return (
    <div className="flex flex-col gap-2.5">
      <div className="flex gap-2">
        <Button size="sm" variant="accent" loading={busy} onClick={install}>
          {busy ? "正在安装…" : "安装"}
        </Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={pick}>
          已安装在别处
        </Button>
      </div>
      {(chunks.length > 0 || error) && (
        <div className="max-h-40 overflow-auto rounded-lg border border-border bg-console p-2.5">
          <Output chunks={chunks} />
          {error && <div className="ansi-out text-danger">{error}</div>}
        </div>
      )}
    </div>
  );
}

function LoginStep({ auth, onRecheck }: { auth: AuthState | null; onRecheck: () => Promise<void> }) {
  const [url, setUrl] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [adc, setAdc] = useState(false);

  if (auth === null) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Spinner /> 正在检查登录状态…
      </div>
    );
  }

  async function start() {
    setBusy(true);
    setError(null);
    try {
      const u = await Colab.loginStart();
      if (!u) {
        await onRecheck(); // signed in already
        return;
      }
      setUrl(u);
      await Colab.openExternal(u);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function finish() {
    setBusy(true);
    setError(null);
    try {
      const st = await Colab.loginFinish(code);
      if (st.error) toast("info", st.error);
      setUrl(null);
      setCode("");
      await onRecheck();
    } catch (e) {
      setError(errorMessage(e));
      setUrl(null);
      setCode("");
    } finally {
      setBusy(false);
    }
  }

  async function useADC() {
    try {
      const s = await Colab.getSettings();
      await Colab.saveSettings({ ...s, auth: "adc" });
      await onRecheck();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  return (
    <div className="flex flex-col gap-2.5">
      {auth.error && !auth.needsLogin && <div className="text-xs break-words text-danger">{auth.error}</div>}
      {url === null ? (
        <div className="flex gap-2">
          <Button size="sm" variant="accent" loading={busy} icon={<LogIn className="size-3.5" />} onClick={start}>
            登录 Google
          </Button>
          {!auth.needsLogin && <Recheck onRecheck={onRecheck} label="重试" />}
        </div>
      ) : (
        <>
          <div className="text-xs text-muted-foreground">在浏览器里同意授权后，把页面上显示的授权码粘贴到这里：</div>
          <div className="flex gap-2">
            <input
              className={cx(inputClass, "font-mono text-xs")}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && code.trim() && void finish()}
              placeholder="授权码"
              autoFocus
              spellCheck={false}
            />
            <Button size="sm" variant="accent" loading={busy} disabled={!code.trim()} onClick={finish}>
              完成
            </Button>
          </div>
          <div className="flex gap-3 text-[11px]">
            <button className="text-muted-foreground hover:text-foreground" onClick={() => Colab.openExternal(url)}>
              重新打开登录页
            </button>
            <button
              className="text-muted-foreground hover:text-foreground"
              onClick={() => {
                void Colab.loginCancel();
                setUrl(null);
              }}
            >
              取消
            </button>
          </div>
        </>
      )}
      {error && <div className="text-xs break-words text-danger">{error}</div>}
      <button className="self-start text-[11px] text-muted-foreground hover:text-foreground" onClick={() => setAdc((v) => !v)}>
        {adc ? "收起" : "改用 gcloud 登录（ADC）"}
      </button>
      {adc && (
        <div className="flex flex-col gap-2">
          <CopyLine text={ADC_LOGIN} />
          <div>
            <Button size="sm" onClick={useADC}>
              已运行，改用 ADC
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
