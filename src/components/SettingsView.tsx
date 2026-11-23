import { useEffect, useState } from "react";
import { BookOpen, CheckCircle2, ExternalLink, FolderSearch, Save, XCircle } from "lucide-react";
import { Colab, errorMessage, type Account, type Environment, type HFTokenStatus, type Settings } from "../lib/api";
import { toast } from "../lib/store";
import { Button, Card, Field, Segmented, Spinner, inputClass } from "./ui";

function HFCard({
  settings,
  onChange,
  onSave,
  saving,
}: {
  settings: Settings;
  onChange: (s: Settings) => void;
  onSave: () => void;
  saving: boolean;
}) {
  const [status, setStatus] = useState<HFTokenStatus | null>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    Colab.getHFToken().then(setStatus);
  }, []);

  async function apply(value: string) {
    setBusy(true);
    try {
      setStatus(await Colab.setHFToken(value));
      setToken("");
    } catch (e) {
      toast("error", "保存 token 失败", errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="mt-4 p-5">
      <div className="text-[13px] font-semibold">HuggingFace</div>
      <div className="mt-4 flex flex-col gap-4">
        <Field label="Access token" hint={status?.set ? `已保存 · ${status.user || "无法验证"}` : "下载需授权的模型时使用，保存在系统凭据管理器中"}>
          <div className="flex gap-2">
            <input className={inputClass} type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder={status?.set ? "••••••••" : "hf_…"} autoComplete="off" />
            <Button loading={busy} disabled={!token.trim()} onClick={() => apply(token)}>
              保存
            </Button>
            {status?.set && (
              <Button variant="ghost" disabled={busy} onClick={() => apply("")}>
                移除
              </Button>
            )}
          </div>
        </Field>
        <Field label="搜索地址" hint="留空为 https://huggingface.co">
          <div className="flex gap-2">
            <input className={inputClass} value={settings.hfEndpoint} onChange={(e) => onChange({ ...settings, hfEndpoint: e.target.value })} placeholder="https://huggingface.co" />
            <Button loading={saving} onClick={onSave}>
              保存
            </Button>
          </div>
        </Field>
      </div>
    </Card>
  );
}

export function SettingsView({
  env,
  account,
  accountError,
  onSaved,
  onCheckAccount,
}: {
  env: Environment | null;
  account: Account | null;
  accountError: string | null;
  onSaved: () => void;
  onCheckAccount: () => void;
}) {
  const [s, setS] = useState<Settings | null>(null);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    Colab.getSettings().then(setS);
  }, []);
  if (!s) return <Spinner className="m-8" />;

  async function save() {
    setSaving(true);
    try {
      await Colab.saveSettings(s!);
      toast("success", "设置已保存");
      onSaved();
    } catch (e) {
      toast("error", "保存失败", errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[760px] px-8 py-7">
        <h1 className="text-xl font-semibold">设置</h1>

        <Card className="mt-5 p-5">
          <div className="text-[13px] font-semibold">Colab CLI</div>
          <div className="mt-3 flex items-center gap-2 text-xs">
            {env === null ? (
              <Spinner />
            ) : env.ok ? (
              <CheckCircle2 className="size-4 text-success" />
            ) : (
              <XCircle className="size-4 text-danger" />
            )}
            {env?.ok ? (
              <span>
                google-colab-cli <b>{env.version}</b> · <span className="font-mono text-muted-foreground">{env.path}</span>
              </span>
            ) : (
              <span className="text-danger">{env?.error}</span>
            )}
          </div>
          <div className="mt-4 flex flex-col gap-4">
            <Field label="colab 可执行文件" hint="留空则从 PATH 和 ~/.local/bin 中查找（uv tool install 的默认位置）">
              <div className="flex gap-2">
                <input className={inputClass} value={s.colabPath} onChange={(e) => setS({ ...s, colabPath: e.target.value })} placeholder="自动检测" />
                <Button
                  icon={<FolderSearch className="size-3.5" />}
                  onClick={async () => {
                    const p = await Colab.pickExecutable();
                    if (p) setS({ ...s, colabPath: p });
                  }}
                >
                  浏览
                </Button>
              </div>
            </Field>
            <Field
              label="认证方式（--auth）"
              hint={
                s.auth === "adc"
                  ? "使用 gcloud 的 Application Default Credentials，需要包含 colaboratory 与 userinfo.email 作用域。"
                  : "默认：首次使用时在浏览器中完成 Google 授权，令牌缓存在本机。"
              }
            >
              <Segmented
                value={s.auth || "oauth2"}
                onChange={(v) => setS({ ...s, auth: v })}
                options={[
                  { value: "oauth2", label: "OAuth2（浏览器）" },
                  { value: "adc", label: "ADC（gcloud）" },
                ]}
              />
            </Field>
            <Field label="会话状态文件（--config）" hint="默认 ~/.config/colab-cli/sessions.json。可用于隔离不同的工作区。">
              <input className={inputClass} value={s.configPath} onChange={(e) => setS({ ...s, configPath: e.target.value })} placeholder="默认" />
            </Field>
            <Field label="OAuth 客户端配置（-c）" hint="默认 ~/.colab-cli-oauth-config.json，不存在时使用 CLI 内置配置。">
              <input className={inputClass} value={s.oauthConfig} onChange={(e) => setS({ ...s, oauthConfig: e.target.value })} placeholder="默认" />
            </Field>
            <Field label="会话列表自动刷新（秒）" hint="0 表示不自动刷新。每次刷新会调用 colab status 与 colab sessions。">
              <input
                className={inputClass + " w-32"}
                type="number"
                min={0}
                value={s.refreshSeconds}
                onChange={(e) => setS({ ...s, refreshSeconds: Number(e.target.value) || 0 })}
              />
            </Field>
          </div>
          <div className="mt-5 flex justify-end">
            <Button variant="primary" loading={saving} icon={<Save className="size-3.5" />} onClick={save}>
              保存
            </Button>
          </div>
        </Card>

        <HFCard settings={s} onChange={setS} onSave={save} saving={saving} />

        <Card className="mt-4 p-5">
          <div className="flex items-center justify-between">
            <div className="text-[13px] font-semibold">账号</div>
            <Button size="sm" onClick={onCheckAccount}>
              重新检查
            </Button>
          </div>
          {account ? (
            <div className="mt-3 grid grid-cols-[100px_1fr] gap-y-1.5 text-xs">
              <span className="text-muted-foreground">邮箱</span>
              <span className="selectable">{account.email}</span>
              <span className="text-muted-foreground">认证方式</span>
              <span>{account.provider}</span>
              <span className="text-muted-foreground">令牌有效期</span>
              <span>{account.expiresIn}</span>
              <span className="text-muted-foreground">作用域</span>
              <span className="selectable font-mono text-[11px] leading-relaxed text-muted-foreground">{account.scopes.join("\n")}</span>
            </div>
          ) : accountError ? (
            <div className="mt-3 text-xs break-words whitespace-pre-wrap text-danger">{accountError}</div>
          ) : (
            <Spinner className="mt-3" />
          )}
        </Card>

        <Card className="mt-4 p-5">
          <div className="text-[13px] font-semibold">链接</div>
          <div className="mt-3 flex flex-wrap gap-2">
            <Button size="sm" icon={<ExternalLink className="size-3.5" />} onClick={() => Colab.openExternal("https://colab.research.google.com/signup")}>
              管理计算单元（colab pay）
            </Button>
            <Button size="sm" icon={<BookOpen className="size-3.5" />} onClick={() => Colab.openExternal("https://github.com/googlecolab/google-colab-cli")}>
              google-colab-cli 文档
            </Button>
          </div>
        </Card>
      </div>
    </div>
  );
}
