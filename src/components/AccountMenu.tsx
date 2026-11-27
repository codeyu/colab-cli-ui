import { useEffect, useRef, useState } from "react";
import { Check, ChevronUp, LogIn, Plus, Trash2 } from "lucide-react";
import { Colab, errorMessage, type AccountUsage, type AccountsState, type SavedAccount } from "../lib/api";
import { cx } from "../lib/format";
import { toast } from "../lib/store";
import { Button, Dialog, Spinner, inputClass } from "./ui";

function label(a: SavedAccount): string {
  return a.email || (a.id === "default" ? "默认账号" : "未知账号");
}

/** The account in the sidebar's footer, which opens the list to switch. */
export function AccountMenu({ email, running, onChanged }: { email: string; running: number; onChanged: () => void }) {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<AccountsState | null>(null);
  const [usage, setUsage] = useState<Record<string, AccountUsage> | null>(null);
  const [adding, setAdding] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  async function load() {
    setState(await Colab.accounts());
    setUsage(null);
    const list = await Colab.accountsUsage();
    setUsage(Object.fromEntries(list.map((u) => [u.id, u])));
  }

  useEffect(() => {
    if (!open) return;
    void load();
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    window.addEventListener("mousedown", close);
    return () => window.removeEventListener("mousedown", close);
  }, [open]);

  async function pick(a: SavedAccount) {
    if (!state || a.id === state.active) return;
    setOpen(false);
    try {
      await Colab.switchAccount(a.id);
      if (running > 0) toast("info", `${email || "原账号"} 还有 ${running} 台虚拟机在运行`, "它们仍在计费，切回该账号可以管理");
      toast("success", `已切换到 ${label(a)}`);
      onChanged();
    } catch (e) {
      toast("error", "切换失败", errorMessage(e));
    }
  }

  async function remove(a: SavedAccount) {
    try {
      await Colab.removeAccount(a.id);
      toast("success", `已移除 ${label(a)}`, "它的登录凭据已从系统凭据管理器删除");
      void load();
    } catch (e) {
      toast("error", "移除失败", errorMessage(e));
    }
  }

  return (
    <div ref={ref} className="relative min-w-0 flex-1">
      <button
        onClick={() => setOpen((o) => !o)}
        title="切换账号"
        className="flex w-full min-w-0 items-center gap-1 rounded-md px-1 py-1 text-[11px] text-muted-foreground hover:bg-sidebar-accent/70 hover:text-foreground"
      >
        <span className="truncate">{email || "账号"}</span>
        <ChevronUp className="size-3 shrink-0" />
      </button>
      {open && (
        <div className="fade-in absolute bottom-full left-0 z-40 mb-1 w-72 rounded-xl border border-border bg-popover p-1 shadow-xl">
          {state === null ? (
            <div className="p-3">
              <Spinner />
            </div>
          ) : (
            state.accounts.map((a) => {
              const u = usage?.[a.id];
              const active = a.id === state.active;
              return (
                <div
                  key={a.id}
                  className={cx("group flex items-center gap-2 rounded-lg px-2 py-1.5", active ? "bg-muted" : "cursor-pointer hover:bg-muted")}
                  onClick={() => void pick(a)}
                >
                  <Check className={cx("size-3.5 shrink-0", active ? "text-accent" : "invisible")} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[13px]">{label(a)}</div>
                    <div className="text-[11px] text-muted-foreground">
                      {usage === null ? (
                        "读取中…"
                      ) : !u ? (
                        ""
                      ) : u.error ? (
                        <span className="text-warning">{u.error}</span>
                      ) : (
                        <>
                          <span className="tabular-nums">{u.balance.toFixed(2)} CU</span>
                          {u.assignments > 0 && (
                            <span className="text-warning">
                              {" "}
                              · {u.assignments} 台运行中 · {u.ratePerHour.toFixed(2)}/小时
                            </span>
                          )}
                        </>
                      )}
                    </div>
                  </div>
                  {!active && a.id !== "default" && (
                    <button
                      title="移除这个账号"
                      className="invisible text-muted-foreground group-hover:visible hover:text-danger"
                      onClick={(e) => {
                        e.stopPropagation();
                        void remove(a);
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </button>
                  )}
                </div>
              );
            })
          )}
          <div className="mt-1 border-t border-border pt-1">
            <button
              className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[13px] hover:bg-muted"
              onClick={() => {
                setOpen(false);
                setAdding(true);
              }}
            >
              <Plus className="size-3.5 text-muted-foreground" /> 添加账号
            </button>
            <div className="px-2 pt-0.5 pb-1 text-[11px] text-muted-foreground">终端里的 colab 命令始终使用默认账号。</div>
          </div>
        </div>
      )}
      <AddAccountDialog
        open={adding}
        onClose={() => setAdding(false)}
        onAdded={(who) => {
          setAdding(false);
          toast("success", `已添加并切换到 ${who}`);
          onChanged();
        }}
      />
    </div>
  );
}

function AddAccountDialog({ open, onClose, onAdded }: { open: boolean; onClose: () => void; onAdded: (email: string) => void }) {
  const [url, setUrl] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setUrl(null);
      setCode("");
      setError(null);
    }
  }, [open]);

  function close() {
    if (url) void Colab.loginCancel();
    onClose();
  }

  async function start() {
    setBusy(true);
    setError(null);
    try {
      const u = await Colab.addAccountStart();
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
      onAdded(st.account.email || "新账号");
    } catch (e) {
      setError(errorMessage(e));
      setUrl(null);
      setCode("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog
      open={open}
      onClose={close}
      title="添加 Google 账号"
      description="每个账号的登录单独保存在系统凭据管理器里，会话和计算单元互不影响。"
      width={460}
      footer={
        <Button variant="ghost" onClick={close}>
          取消
        </Button>
      }
    >
      <div className="flex flex-col gap-2.5">
        {url === null ? (
          <div>
            <Button variant="accent" loading={busy} icon={<LogIn className="size-3.5" />} onClick={start}>
              打开 Google 登录
            </Button>
            <div className="mt-2 text-[11px] text-muted-foreground">在授权页面选择要添加的账号，并勾选全部权限。</div>
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
              <Button variant="accent" loading={busy} disabled={!code.trim()} onClick={finish}>
                完成
              </Button>
            </div>
            <button className="self-start text-[11px] text-muted-foreground hover:text-foreground" onClick={() => Colab.openExternal(url)}>
              重新打开登录页
            </button>
          </>
        )}
        {error && <div className="text-xs break-words text-danger">{error}</div>}
      </div>
    </Dialog>
  );
}
