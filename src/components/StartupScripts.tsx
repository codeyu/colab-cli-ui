import { useEffect, useState } from "react";
import { ChevronDown, ChevronRight, Save, Trash2 } from "lucide-react";
import { Colab, errorMessage, type HistoryEvent, type StartupScript } from "../lib/api";
import { cx, dateTime } from "../lib/format";
import { toast } from "../lib/store";
import { Button, Card, Dialog, IconButton, Spinner, Toggle, inputClass } from "./ui";

/** Picks steps from a session's history and saves them as a startup script. */
export function SaveScriptDialog({
  open,
  onClose,
  source,
  steps,
  drive,
}: {
  open: boolean;
  onClose: () => void;
  source: string;
  steps: HistoryEvent[];
  drive: boolean;
}) {
  const [name, setName] = useState("");
  const [picked, setPicked] = useState<Set<number>>(new Set());
  const [mount, setMount] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setName(`${source} 启动脚本`);
    setPicked(new Set(steps.map((_, i) => i)));
    setMount(drive);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  async function save() {
    setSaving(true);
    try {
      await Colab.saveScript({
        id: "",
        name,
        drive: mount,
        steps: steps.filter((_, i) => picked.has(i)).map((e) => ({ code: e.code! })),
        updated: new Date().toISOString(),
      });
      toast("success", `已保存「${name}」`, "新建会话时可以选择它");
      onClose();
    } catch (e) {
      toast("error", "保存失败", errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="保存为启动脚本"
      width={560}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button variant="accent" loading={saving} disabled={!name.trim() || picked.size === 0} icon={<Save className="size-3.5" />} onClick={save}>
            保存（{picked.size} 步）
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        <div className="max-h-72 overflow-y-auto rounded-lg border border-border">
          {steps.map((e, i) => (
            <label key={i} className={cx("flex cursor-default items-start gap-2.5 px-2.5 py-1.5", i > 0 && "border-t border-border")}>
              <input
                type="checkbox"
                className="mt-0.5 accent-[var(--accent)]"
                checked={picked.has(i)}
                onChange={(ev) =>
                  setPicked((s) => {
                    const n = new Set(s);
                    if (ev.target.checked) n.add(i);
                    else n.delete(i);
                    return n;
                  })
                }
              />
              <pre className={cx("line-clamp-3 min-w-0 flex-1 font-mono text-[11px] whitespace-pre-wrap", !picked.has(i) && "text-muted-foreground")}>
                {e.code!.trim()}
              </pre>
              <span className="shrink-0 text-[10px] text-muted-foreground tabular-nums">{dateTime(e.timestamp).split(" ")[1]}</span>
            </label>
          ))}
        </div>
        <Toggle checked={mount} onChange={setMount} label="先挂载 Google Drive" />
      </div>
    </Dialog>
  );
}

/** The saved startup scripts, for the settings page. */
export function ScriptsCard() {
  const [list, setList] = useState<StartupScript[] | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const load = () =>
    Colab.listScripts()
      .then(setList)
      .catch((e) => toast("error", "读取启动脚本失败", errorMessage(e)));
  useEffect(() => {
    void load();
  }, []);

  async function remove(s: StartupScript) {
    try {
      await Colab.deleteScript(s.id);
      await load();
    } catch (e) {
      toast("error", "删除失败", errorMessage(e));
    }
  }

  return (
    <Card className="mt-4 p-5">
      <div className="text-[13px] font-semibold">启动脚本</div>
      <div className="mt-0.5 text-[11px] text-muted-foreground">在历史记录里挑选步骤保存；新建会话时选择后自动执行</div>
      <div className="mt-3">
        {list === null && <Spinner />}
        {list?.length === 0 && <div className="text-xs text-muted-foreground">还没有</div>}
        {list?.map((s) => (
          <div key={s.id} className="rounded-lg hover:bg-muted/50">
            <div className="flex items-center gap-2 px-2 py-1.5">
              <button className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => setOpen(open === s.id ? null : s.id)}>
                {open === s.id ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5 text-muted-foreground" />}
                <span className="truncate text-[13px]">{s.name}</span>
                <span className="text-[11px] text-muted-foreground">
                  {s.steps.length} 步{s.drive ? " · Drive" : ""}
                </span>
              </button>
              <IconButton label="删除" className="size-6 hover:text-danger" onClick={() => remove(s)}>
                <Trash2 className="size-3.5" />
              </IconButton>
            </div>
            {open === s.id && (
              <div className="mx-2 mb-2 ml-8 overflow-hidden rounded-lg border border-border">
                {s.steps.map((st, i) => (
                  <pre key={i} className={cx("selectable max-h-40 overflow-auto bg-muted/40 px-3 py-2 font-mono text-[11px]", i > 0 && "border-t border-border")}>
                    {st.code.trim()}
                  </pre>
                ))}
              </div>
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}
