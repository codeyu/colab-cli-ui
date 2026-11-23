import { useEffect, useState } from "react";
import { Play } from "lucide-react";
import { Colab, type HistoryEvent, type Session } from "../lib/api";
import { runSteps } from "../lib/startup";
import { cx, randomName } from "../lib/format";
import { cellsStore, toast } from "../lib/store";

import { runCell } from "./ConsoleTab";

const NEW = "__new__";
import { Button, Dialog, Toggle, inputClass } from "./ui";

export function ReplayDialog({
  open,
  onClose,
  source,
  steps,
  drive,
  targets,
  origin,
  onGo,
  onSessionsChanged,
}: {
  open: boolean;
  onClose: () => void;
  source: string;
  steps: HistoryEvent[];
  drive: boolean;
  targets: Session[];
  /** The original session's hardware, to create one like it. */
  origin: { accelerator: string; highMem: boolean };
  onGo: (target: string) => void;
  onSessionsChanged: () => Promise<void>;
}) {
  const running = targets.filter((s) => s.local);
  const hw = origin.accelerator === "NONE" || !origin.accelerator ? "CPU" : origin.accelerator;
  const [target, setTarget] = useState("");
  const [stopOnError, setStopOnError] = useState(true);

  useEffect(() => {
    if (open) setTarget(running.find((s) => s.name === source)?.name ?? running[0]?.name ?? NEW);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  async function start() {
    onClose();
    let name = target;
    if (target === NEW) {
      name = randomName(source.slice(0, 20));
      toast("info", `正在新建 ${hw} 会话 ${name}…`);
      const ok = await runCell(name, { kind: "create", title: `新建 ${hw} 会话` }, (signal, ch) =>
        Colab.createSession.with({ signal })({ name, accelerator: hw, highMem: origin.highMem }, ch),
      );
      if (!ok) {
        const err = (cellsStore.get()[name] ?? []).at(-1)?.error;
        toast("error", "新建会话失败", err);
        return;
      }
      await onSessionsChanged();
    }
    onGo(name);
    void runSteps(name, steps.map((e) => e.code!), { label: "重新执行", stopOnError, drive });
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={steps.length === 1 ? "重新执行这段代码？" : `确定全部执行吗？（${steps.length} 段）`}
      width={480}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button variant="accent" icon={<Play className="size-3.5" />} disabled={!target} onClick={start}>
            执行
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
          <select className={inputClass} value={target} onChange={(e) => setTarget(e.target.value)}>
            {running.map((s) => (
              <option key={s.name} value={s.name}>
                {s.name} · {s.hardware}
              </option>
            ))}
            <option value={NEW}>新建 {hw}{origin.highMem ? " 高内存" : ""} 会话（按原配置）</option>
          </select>
          <div className="max-h-40 overflow-y-auto rounded-lg border border-border">
            {steps.map((e, i) => (
              <div key={i} className={cx("truncate px-2.5 py-1 font-mono text-[11px] text-muted-foreground", i > 0 && "border-t border-border")}>
                {i + 1}. {e.code!.trim().split("\n")[0]}
              </div>
            ))}
          </div>
          {steps.length > 1 && <Toggle checked={stopOnError} onChange={setStopOnError} label="出错时停止" />}
          {drive && <div className="text-xs text-warning">需要挂载 Drive：执行前会打开授权终端，挂载完成后自动继续。</div>}
      </div>
    </Dialog>
  );
}
