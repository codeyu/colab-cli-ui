import { useState } from "react";
import { CheckCircle2, ChevronDown, ChevronRight, Trash2, X, XCircle } from "lucide-react";
import { clock, cx, duration } from "../lib/format";
import { activityStore, useStore } from "../lib/store";
import { IconButton, Spinner } from "./ui";

/** The CLI commands the app ran, like t3code's terminal drawer. */
export function ActivityDrawer({ onClose }: { onClose: () => void }) {
  const list = useStore(activityStore);
  const [open, setOpen] = useState<number | null>(null);
  return (
    <div className="flex h-64 shrink-0 flex-col border-t border-border bg-sidebar">
      <div className="flex items-center gap-2 px-4 py-1.5">
        <span className="text-xs font-medium">CLI 命令日志</span>
        <span className="text-[11px] text-muted-foreground">界面执行的每一条 colab 命令</span>
        <div className="flex-1" />
        <IconButton label="清空" className="size-6" onClick={() => activityStore.set([])}>
          <Trash2 className="size-3.5" />
        </IconButton>
        <IconButton label="关闭" className="size-6" onClick={onClose}>
          <X className="size-3.5" />
        </IconButton>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2 font-mono text-[11.5px]">
        {list.length === 0 && <div className="px-2 py-2 text-muted-foreground">暂无</div>}
        {[...list].reverse().map((e) => (
          <div key={e.id}>
            <button
              onClick={() => setOpen(open === e.id ? null : e.id)}
              className={cx("flex w-full items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-muted", open === e.id && "bg-muted")}
            >
              {open === e.id ? <ChevronDown className="size-3 shrink-0" /> : <ChevronRight className="size-3 shrink-0 text-muted-foreground" />}
              {e.running ? (
                <Spinner className="size-3 text-accent" />
              ) : e.exitCode === 0 ? (
                <CheckCircle2 className="size-3 shrink-0 text-success" />
              ) : (
                <XCircle className="size-3 shrink-0 text-danger" />
              )}
              <span className="shrink-0 text-muted-foreground">{clock(e.started)}</span>
              <span className="min-w-0 flex-1 truncate">{e.command}</span>
              <span className="shrink-0 text-muted-foreground">{e.running ? "…" : `${duration(e.duration)}${e.exitCode ? ` · exit ${e.exitCode}` : ""}`}</span>
            </button>
            {open === e.id && (
              <pre className="selectable mx-2 my-1 max-h-40 overflow-auto rounded-md border border-border bg-console p-2 whitespace-pre-wrap">
                {e.output || (e.running ? "运行中…" : "（无输出）")}
              </pre>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
