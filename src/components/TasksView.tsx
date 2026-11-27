import { useEffect, useMemo, useState } from "react";
import {
  AudioLines,
  Bot,
  ChevronDown,
  ChevronRight,
  Download,
  FileUp,
  FolderOpen,
  FolderUp,
  Hammer,
  Library,
  Pencil,
  Play,
  Plus,
  Square,
  Trash2,
  Upload,
  Wrench,
  X,
} from "lucide-react";
import { onFileDrop } from "mygo-runtime";
import { Colab, errorMessage, type Session, type StartupScript, type TaskDef, type Usage } from "../lib/api";
import { ACCELERATORS, costLine, eligibilityOf } from "../lib/accelerators";
import { cx, randomName } from "../lib/format";
import { runSteps } from "../lib/startup";
import { cellsStore, toast, useStore, type Cell } from "../lib/store";
import { runCell } from "./ConsoleTab";
import { Output } from "./Output";
import { openOnTab } from "./SessionView";
import { Badge, Button, Card, Dialog, Empty, Field, IconButton, Spinner, Toggle, inputClass } from "./ui";

const NEW = "__new__";

const KINDS: Record<string, { label: string; icon: React.ReactNode }> = {
  batch: { label: "批处理", icon: <Hammer className="size-4" /> },
  service: { label: "服务", icon: <Bot className="size-4" /> },
  setup: { label: "准备", icon: <Wrench className="size-4" /> },
};

const ICONS: Record<string, React.ReactNode> = {
  "asr-batch": <AudioLines className="size-4" />,
  "llm-api": <Bot className="size-4" />,
};

/** A startup script, shown in the library as a setup task. */
function scriptTask(s: StartupScript): TaskDef {
  return {
    id: "script:" + s.id,
    kind: "setup",
    name: s.name,
    category: "启动脚本",
    description: `${s.steps.length} 段代码${s.drive ? "，先挂载 Google Drive" : ""}。在「历史记录」里挑选步骤保存。`,
    builtin: false,
    hardware: "CPU",
    params: [],
    steps: s.steps.map((x) => x.code),
    drive: s.drive,
  };
}

interface Host {
  sessions: Session[];
  usage: Usage | null;
  onGo: (name: string) => void;
  onSessionsChanged: () => Promise<void>;
}

export function TasksView(host: Host) {
  const [tasks, setTasks] = useState<TaskDef[] | null>(null);
  const [category, setCategory] = useState("全部");
  const [running, setRunning] = useState<TaskDef | null>(null);
  const [editing, setEditing] = useState<TaskDef | null>(null);

  async function load() {
    try {
      const [list, scripts] = await Promise.all([Colab.listTasks(), Colab.listScripts().catch(() => [])]);
      setTasks([...list, ...scripts.map(scriptTask)]);
    } catch (e) {
      toast("error", "读取任务库失败", errorMessage(e));
      setTasks([]);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const categories = useMemo(() => ["全部", ...new Set((tasks ?? []).map((t) => t.category || "其他"))], [tasks]);
  const shown = (tasks ?? []).filter((t) => category === "全部" || (t.category || "其他") === category);

  async function importTask() {
    try {
      const t = await Colab.importTask();
      if (t) setEditing(t);
    } catch (e) {
      toast("error", "导入失败", errorMessage(e));
    }
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-5xl px-8 py-7">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h1 className="text-[22px] font-semibold">任务库</h1>
            <p className="mt-1 text-xs text-muted-foreground">在 Colab 上做过的事，保存下来一键再跑：选好参数和文件，自动开机、处理、取回结果、释放虚拟机。</p>
          </div>
          <div className="flex shrink-0 gap-2">
            <Button size="sm" icon={<Upload className="size-3.5" />} onClick={importTask}>
              导入
            </Button>
            <Button size="sm" variant="accent" icon={<Plus className="size-3.5" />} onClick={() => setEditing(blankTask())}>
              新建任务
            </Button>
          </div>
        </div>

        <Runs />

        <div className="mt-6 flex flex-wrap gap-1.5">
          {categories.map((c) => (
            <button
              key={c}
              onClick={() => setCategory(c)}
              className={cx("rounded-full border px-3 py-1 text-xs", category === c ? "border-accent bg-accent-soft text-foreground" : "border-border text-muted-foreground hover:bg-muted")}
            >
              {c}
            </button>
          ))}
        </div>

        {tasks === null ? (
          <div className="mt-6">
            <Spinner />
          </div>
        ) : shown.length === 0 ? (
          <div className="mt-6">
            <Empty icon={<Library className="size-5" />} title="这个分类里还没有任务" />
          </div>
        ) : (
          <div className="mt-3 grid grid-cols-2 gap-3">
            {shown.map((t) => (
              <TaskCard
                key={t.id}
                t={t}
                onRun={() => setRunning(t)}
                onEdit={t.builtin || t.id.startsWith("script:") ? undefined : () => setEditing(t)}
                onDelete={
                  t.builtin
                    ? undefined
                    : async () => {
                        try {
                          if (t.id.startsWith("script:")) await Colab.deleteScript(t.id.slice(7));
                          else await Colab.deleteTask(t.id);
                          void load();
                        } catch (e) {
                          toast("error", "删除失败", errorMessage(e));
                        }
                      }
                }
                onExport={
                  t.kind === "service" || t.id.startsWith("script:")
                    ? undefined
                    : async () => {
                        try {
                          const p = await Colab.exportTask(t.id);
                          if (p) toast("success", "已导出", p);
                        } catch (e) {
                          toast("error", "导出失败", errorMessage(e));
                        }
                      }
                }
              />
            ))}
          </div>
        )}
      </div>

      {running && <RunDialog t={running} host={host} onClose={() => setRunning(null)} />}
      {editing && (
        <TaskEditor
          t={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            void load();
          }}
        />
      )}
    </div>
  );
}

function TaskCard({
  t,
  onRun,
  onEdit,
  onDelete,
  onExport,
}: {
  t: TaskDef;
  onRun: () => void;
  onEdit?: () => void;
  onDelete?: () => void;
  onExport?: () => void;
}) {
  const kind = KINDS[t.kind] ?? KINDS.batch!;
  const [confirm, setConfirm] = useState(false);
  return (
    <Card className="flex flex-col p-4">
      <div className="flex items-center gap-2">
        <span className="text-accent">{ICONS[t.id] ?? kind.icon}</span>
        <span className="truncate text-[14px] font-semibold">{t.name}</span>
        <Badge className="ml-auto shrink-0">{kind.label}</Badge>
        {t.category && <Badge className="shrink-0">{t.category}</Badge>}
      </div>
      <p className="mt-2 line-clamp-3 flex-1 text-xs leading-relaxed text-muted-foreground">{t.description}</p>
      <div className="mt-3 flex items-center gap-1">
        <span className="text-[11px] text-muted-foreground">建议 {t.hardware}</span>
        <div className="ml-auto flex items-center gap-0.5">
          {onExport && (
            <IconButton label="导出" onClick={onExport}>
              <Download className="size-3.5" />
            </IconButton>
          )}
          {onEdit && (
            <IconButton label="编辑" onClick={onEdit}>
              <Pencil className="size-3.5" />
            </IconButton>
          )}
          {onDelete &&
            (confirm ? (
              <Button size="sm" variant="danger" className="h-7" onClick={onDelete}>
                确定删除
              </Button>
            ) : (
              <IconButton label="删除" onClick={() => setConfirm(true)}>
                <Trash2 className="size-3.5" />
              </IconButton>
            ))}
          <Button size="sm" variant="accent" icon={<Play className="size-3.5" />} onClick={onRun} className="ml-1">
            运行
          </Button>
        </div>
      </div>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Running

function dirOf(p: string): string {
  const i = Math.max(p.lastIndexOf("\\"), p.lastIndexOf("/"));
  return i > 0 ? p.slice(0, i) : p;
}

function joinPath(dir: string, name: string): string {
  const sep = dir.includes("\\") ? "\\" : "/";
  return dir.replace(/[\\/]+$/, "") + sep + name;
}

function baseName(p: string): string {
  return p.replace(/[\\/]+$/, "").split(/[\\/]/).pop() ?? p;
}

function defaults(t: TaskDef): Record<string, string> {
  return Object.fromEntries(t.params.map((p) => [p.id, p.default ?? ""]));
}

function visible(p: TaskDef["params"][number], values: Record<string, string>): boolean {
  if (!p.showIf) return true;
  const [k, v] = p.showIf.split("=");
  return values[k ?? ""] === v;
}

function RunDialog({ t, host, onClose }: { t: TaskDef; host: Host; onClose: () => void }) {
  const running = host.sessions.filter((s) => s.local);
  const [target, setTarget] = useState(t.kind === "batch" || running.length === 0 ? NEW : running[0]!.name);
  const [acc, setAcc] = useState(t.hardware || "CPU");
  const [values, setValues] = useState<Record<string, string>>(() => defaults(t));
  const [inputs, setInputs] = useState<string[]>([]);
  const [outDir, setOutDir] = useState("");
  const [release, setRelease] = useState(true);

  useEffect(() => setRelease(target === NEW), [target]);
  // Files dragged onto the window are added too.
  useEffect(
    () =>
      t.kind === "batch"
        ? onFileDrop(({ paths }) => {
            if (paths.length) setInputs((l) => [...new Set([...l, ...paths])]);
          })
        : undefined,
    [t.kind],
  );
  useEffect(() => {
    if (inputs.length && !outDir) setOutDir(joinPath(dirOf(inputs[0]!), `${t.name}-结果`));
  }, [inputs]); // eslint-disable-line react-hooks/exhaustive-deps

  async function pick(folder: boolean) {
    try {
      const got = await Colab.pickTaskInputs(folder);
      setInputs((l) => [...new Set([...l, ...got])]);
    } catch (e) {
      toast("error", "选择失败", errorMessage(e));
    }
  }

  const ready = t.kind !== "batch" || (inputs.length > 0 && outDir !== "");

  async function start() {
    onClose();
    let name = target;
    if (t.kind === "batch") {
      name = target === NEW ? randomName("task") : target;
      const req = {
        taskId: t.id,
        session: name,
        create: target === NEW ? { name, accelerator: acc, highMem: false } : undefined,
        params: values,
        inputs,
        outputDir: outDir,
        release,
      };
      toast("info", `${t.name} 已开始`, "进度显示在任务库顶部");
      const ok = await runCell(name, { kind: "task", title: t.name, code: outDir }, (ch) => Colab.runTask(req, ch));
      if (ok) toast("success", `${t.name} 完成`, outDir);
      void host.onSessionsChanged();
      return;
    }
    if (target === NEW) {
      name = randomName(t.kind === "service" ? "api" : "s");
      toast("info", `正在新建 ${acc} 会话 ${name}…`);
      const ok = await runCell(name, { kind: "create", title: `新建 ${acc} 会话` }, (ch) => Colab.createSession({ name, accelerator: acc, highMem: false }, ch));
      if (!ok) {
        toast("error", "新建会话失败", (cellsStore.get()[name] ?? []).at(-1)?.error);
        return;
      }
      await host.onSessionsChanged();
    }
    if (t.kind === "service") {
      openOnTab(name, "api");
      host.onGo(name);
      return;
    }
    host.onGo(name);
    void runSteps(name, t.steps ?? [], { label: t.name, drive: t.drive });
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={t.name}
      description={t.description}
      width={600}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button variant="accent" icon={<Play className="size-3.5" />} disabled={!ready} onClick={start}>
            {t.kind === "service" ? "打开" : "开始运行"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="在哪台虚拟机上运行">
          <select className={inputClass} value={target} onChange={(e) => setTarget(e.target.value)}>
            <option value={NEW}>新建会话</option>
            {running.map((s) => (
              <option key={s.name} value={s.name}>
                {s.name} · {s.hardware}
              </option>
            ))}
          </select>
        </Field>
        {target === NEW && (
          <div>
            <div className="flex flex-wrap gap-1.5">
              {ACCELERATORS.map((a) => {
                const blocked = eligibilityOf(host.usage, a.id) === "ineligible";
                return (
                  <button
                    key={a.id}
                    disabled={blocked}
                    onClick={() => setAcc(a.id)}
                    className={cx(
                      "rounded-lg border px-2.5 py-1 text-xs",
                      blocked ? "cursor-not-allowed opacity-35" : acc === a.id ? "border-accent bg-accent-soft" : "border-border hover:bg-muted",
                    )}
                  >
                    {a.label}
                    {a.id === t.hardware && <span className="ml-1 text-[10px] text-accent">建议</span>}
                  </button>
                );
              })}
            </div>
            <div className="mt-1.5 text-[11px] text-muted-foreground">
              {t.hardwareNote ? `${t.hardwareNote}。` : ""}
              <span className="text-foreground">{costLine(acc, false, host.usage?.balance)}</span>
            </div>
          </div>
        )}

        {t.params.filter((p) => visible(p, values)).map((p) => (
          <Field key={p.id} label={p.label} hint={p.help}>
            <ParamInput p={p} value={values[p.id] ?? ""} onChange={(v) => setValues((o) => ({ ...o, [p.id]: v }))} />
          </Field>
        ))}

        {t.kind === "batch" && (
          <>
            <div>
              <div className="mb-1.5 flex items-center gap-2 text-xs font-medium">
                要处理的文件
                <div className="ml-auto flex gap-1.5">
                  <Button size="sm" icon={<FileUp className="size-3.5" />} onClick={() => pick(false)}>
                    选择文件
                  </Button>
                  <Button size="sm" icon={<FolderUp className="size-3.5" />} onClick={() => pick(true)}>
                    选择文件夹
                  </Button>
                </div>
              </div>
              <div className="mb-1.5 text-[11px] text-muted-foreground">
                {t.inputLabel ? `${t.inputLabel}。` : ""}也可以把文件或文件夹拖到这个窗口里。
              </div>
              {inputs.length > 0 && (
                <div className="max-h-32 overflow-y-auto rounded-lg border border-border">
                  {inputs.map((p) => (
                    <div key={p} className="flex items-center gap-2 border-b border-border px-2.5 py-1 text-[12px] last:border-0">
                      <span className="truncate font-mono" title={p}>
                        {baseName(p)}
                      </span>
                      <span className="truncate text-[11px] text-muted-foreground">{dirOf(p)}</span>
                      <button className="ml-auto text-muted-foreground hover:text-danger" onClick={() => setInputs((l) => l.filter((x) => x !== p))}>
                        <X className="size-3.5" />
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
            <Field label={t.outputLabel || "结果保存到"}>
              <div className="flex gap-2">
                <input className={cx(inputClass, "font-mono text-xs")} value={outDir} onChange={(e) => setOutDir(e.target.value)} placeholder="选择文件后自动填写" />
                <Button
                  onClick={async () => {
                    const d = await Colab.pickFolder("结果保存到");
                    if (d) setOutDir(d);
                  }}
                >
                  更改
                </Button>
              </div>
            </Field>
            <Toggle checked={release} onChange={setRelease} label={target === NEW ? "完成后释放虚拟机（失败或取消时也释放）" : "完成后释放这台虚拟机"} />
          </>
        )}
        {t.kind === "service" && <div className="text-[11px] text-muted-foreground">会话准备好后会打开它的「模型 API」页，在那里选模型并部署。</div>}
      </div>
    </Dialog>
  );
}

function ParamInput({ p, value, onChange }: { p: TaskDef["params"][number]; value: string; onChange: (v: string) => void }) {
  const opts = p.options ?? [];
  switch (p.type) {
    case "select":
      return (
        <div>
          <select className={inputClass} value={value} onChange={(e) => onChange(e.target.value)}>
            {opts.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
                {o.hint ? ` — ${o.hint}` : ""}
              </option>
            ))}
          </select>
        </div>
      );
    case "multi": {
      const set = new Set(value.split(",").filter(Boolean));
      return (
        <div className="flex flex-wrap gap-1.5">
          {opts.map((o) => (
            <button
              key={o.value}
              type="button"
              onClick={() => {
                const next = new Set(set);
                if (next.has(o.value)) next.delete(o.value);
                else next.add(o.value);
                onChange(opts.filter((x) => next.has(x.value)).map((x) => x.value).join(","));
              }}
              className={cx("rounded-lg border px-2.5 py-1 text-xs", set.has(o.value) ? "border-accent bg-accent-soft" : "border-border hover:bg-muted")}
            >
              {o.label}
            </button>
          ))}
        </div>
      );
    }
    case "bool":
      return <Toggle checked={value === "true"} onChange={(v) => onChange(v ? "true" : "false")} />;
    case "number":
      return <input className={cx(inputClass, "w-40 font-mono")} inputMode="numeric" value={value} onChange={(e) => onChange(e.target.value.trim())} />;
    default:
      return <input className={inputClass} value={value} onChange={(e) => onChange(e.target.value)} />;
  }
}

// ---------------------------------------------------------------------------
// Runs

function taskProgress(c: Cell): { stage: string; item: string; pct: number | null } {
  const text = c.chunks.map((x) => x.text ?? "").join("");
  const stage = [...text.matchAll(/^== (.+)$/gm)].at(-1)?.[1] ?? "准备中";
  const items = [...text.matchAll(/\[(\d+)\/(\d+)\] ([^\r\n]+)/g)];
  const last = items.at(-1);
  let pct: number | null = null;
  if (last) {
    const after = text.slice((last.index ?? 0) + last[0].length);
    const p = [...after.matchAll(/(\d+)%/g)].at(-1);
    const i = Number(last[1]) - 1 + (p ? Number(p[1]) / 100 : /✓/.test(after) ? 1 : 0);
    pct = Math.min(100, (100 * i) / Number(last[2]));
  }
  return { stage, item: last ? `${last[1]}/${last[2]} ${last[3]}` : "", pct };
}

function Runs() {
  const all = useStore(cellsStore);
  const runs = Object.entries(all)
    .flatMap(([session, cells]) => cells.filter((c) => c.kind === "task").map((c) => ({ session, c })))
    .sort((a, b) => b.c.started - a.c.started);
  if (runs.length === 0) return null;
  return (
    <div className="mt-5">
      <div className="mb-2 text-[13px] font-semibold">运行记录</div>
      <div className="flex flex-col gap-2">
        {runs.map(({ session, c }) => (
          <RunRow key={c.id} session={session} c={c} />
        ))}
      </div>
    </div>
  );
}

function RunRow({ session, c }: { session: string; c: Cell }) {
  const [open, setOpen] = useState(c.status !== "done");
  const p = taskProgress(c);
  const tone = c.status === "running" ? "warning" : c.status === "done" ? "success" : c.status === "canceled" ? "neutral" : "danger";
  const label = { running: p.stage, done: "完成", error: "失败", canceled: "已取消" }[c.status];
  return (
    <Card className="p-3">
      <div className="flex items-center gap-2">
        <button className="text-muted-foreground" onClick={() => setOpen((o) => !o)}>
          {open ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
        </button>
        <span className="text-[13px] font-medium">{c.title}</span>
        <span className="text-[11px] text-muted-foreground">{session}</span>
        <Badge tone={tone as "success"}>{label}</Badge>
        {c.status === "running" && p.item && <span className="truncate text-[11px] text-muted-foreground">{p.item}</span>}
        <div className="ml-auto flex items-center gap-1.5">
          {c.status === "running" ? (
            <Button size="sm" icon={<Square className="size-3.5" />} onClick={() => c.abort?.abort()}>
              取消
            </Button>
          ) : (
            c.code && (
              <Button size="sm" icon={<FolderOpen className="size-3.5" />} onClick={() => Colab.openFolder(c.code!).catch((e) => toast("error", "打开失败", errorMessage(e)))}>
                打开结果
              </Button>
            )
          )}
        </div>
      </div>
      {c.status === "running" && p.pct !== null && (
        <div className="mt-2 h-1 overflow-hidden rounded-full bg-muted">
          <div className="h-full bg-accent transition-all" style={{ width: `${p.pct}%` }} />
        </div>
      )}
      {open && (
        <div className="mt-2 max-h-64 overflow-auto rounded-lg border border-border bg-console p-3">
          <Output chunks={c.chunks} />
          {c.error && <div className="ansi-out selectable mt-1 text-danger">{c.error}</div>}
        </div>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Editing

const TEMPLATE = `# 批处理任务：处理 $TASK_INPUT 里的文件，把结果写进 $TASK_OUTPUT。
# 参数在 JSON 文件 $TASK_PARAMS 里。打印 "[i/n] 文件名" 和 "xx%" 会显示成进度。
import glob, json, os

params = json.load(open(os.environ["TASK_PARAMS"], encoding="utf-8"))
src, dst = os.environ["TASK_INPUT"], os.environ["TASK_OUTPUT"]
files = sorted(p for p in glob.glob(os.path.join(src, "**", "*"), recursive=True) if os.path.isfile(p))
for i, f in enumerate(files, 1):
    print("[%d/%d] %s" % (i, len(files), os.path.relpath(f, src)), flush=True)
    out = os.path.join(dst, os.path.relpath(f, src) + ".txt")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    with open(out, "w", encoding="utf-8") as fh:
        fh.write("%d bytes\\n" % os.path.getsize(f))
`;

function blankTask(): TaskDef {
  return { id: "", kind: "batch", name: "", category: "", description: "", builtin: false, hardware: "CPU", params: [], script: TEMPLATE, inputLabel: "", outputLabel: "" };
}

function TaskEditor({ t, onClose, onSaved }: { t: TaskDef; onClose: () => void; onSaved: () => void }) {
  const imported = !t.id;
  const [d, setD] = useState<TaskDef>(t);
  const [steps, setSteps] = useState((t.steps ?? []).join("\n# %%\n"));
  const [params, setParams] = useState(t.params.length ? JSON.stringify(t.params, null, 2) : "");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof TaskDef) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => setD((o) => ({ ...o, [k]: e.target.value }));

  async function save() {
    setBusy(true);
    setError(null);
    try {
      const def: TaskDef = { ...d, params: params.trim() ? JSON.parse(params) : [] };
      if (d.kind === "setup") {
        def.steps = steps.split(/^# %%.*$/m).map((s) => s.trim()).filter(Boolean);
        def.script = "";
      } else def.steps = [];
      await Colab.saveTask(def);
      toast("success", `已保存「${def.name}」`);
      onSaved();
    } catch (e) {
      setError(e instanceof SyntaxError ? `参数不是有效的 JSON：${e.message}` : errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={imported && t.name ? `导入任务「${t.name}」` : t.id ? `编辑「${t.name}」` : "新建任务"}
      description={imported && t.name ? "看一下脚本再添加：它会在你付费的虚拟机上运行。" : undefined}
      width={720}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button variant="accent" loading={busy} disabled={!d.name.trim()} onClick={save}>
            {imported && t.name ? "添加到任务库" : "保存"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-[1fr_140px_120px_110px] gap-2">
          <Field label="名称">
            <input className={inputClass} value={d.name} onChange={set("name")} autoFocus />
          </Field>
          <Field label="分类">
            <input className={inputClass} value={d.category} onChange={set("category")} placeholder="例如 语音" />
          </Field>
          <Field label="类型">
            <select className={inputClass} value={d.kind} onChange={set("kind")}>
              <option value="batch">批处理</option>
              <option value="setup">准备（在内核里运行）</option>
            </select>
          </Field>
          <Field label="建议硬件">
            <select className={inputClass} value={d.hardware} onChange={set("hardware")}>
              {ACCELERATORS.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.label}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field label="说明">
          <input className={inputClass} value={d.description} onChange={set("description")} />
        </Field>
        {d.kind === "batch" ? (
          <>
            <Field label="脚本（Python）" hint="在 VM 后台运行，连接断开也不影响">
              <textarea className={cx(inputClass, "h-64 resize-y py-2 font-mono text-xs leading-relaxed")} value={d.script ?? ""} onChange={set("script")} spellCheck={false} />
            </Field>
            <div className="grid grid-cols-2 gap-2">
              <Field label="输入说明">
                <input className={inputClass} value={d.inputLabel ?? ""} onChange={set("inputLabel")} placeholder="例如 图片文件" />
              </Field>
              <Field label="输出说明">
                <input className={inputClass} value={d.outputLabel ?? ""} onChange={set("outputLabel")} placeholder="例如 处理结果保存到" />
              </Field>
            </div>
          </>
        ) : (
          <Field label="代码" hint="多段代码之间用一行 # %% 分隔，依次在内核里运行">
            <textarea className={cx(inputClass, "h-64 resize-y py-2 font-mono text-xs leading-relaxed")} value={steps} onChange={(e) => setSteps(e.target.value)} spellCheck={false} />
          </Field>
        )}
        <Field label="参数（可选，JSON）" hint='例如 [{"id": "size", "label": "尺寸", "type": "select", "default": "1024", "options": [{"value": "1024", "label": "1024"}]}]'>
          <textarea className={cx(inputClass, "h-20 resize-y py-2 font-mono text-xs")} value={params} onChange={(e) => setParams(e.target.value)} spellCheck={false} />
        </Field>
        {error && <div className="text-xs text-danger">{error}</div>}
      </div>
    </Dialog>
  );
}
