import { useState } from "react";
import { Cable, Copy, ExternalLink, Plus, X } from "lucide-react";
import { Colab, errorMessage, type Forward } from "../lib/api";
import { forwardURL, useForwards } from "../lib/forwards";
import { cx } from "../lib/format";
import { toast } from "../lib/store";
import { Badge, Button, Card, Empty, IconButton, StatusDot, inputClass } from "./ui";

const PRESETS: { label: string; port: number }[] = [
  { label: "Gradio", port: 7860 },
  { label: "Streamlit", port: 8501 },
  { label: "ComfyUI", port: 8188 },
  { label: "TensorBoard", port: 6006 },
  { label: "Ollama", port: 11434 },
  { label: "vLLM / FastAPI", port: 8000 },
];

/** Opens a forward and reports how it went; resolves to it, or null. */
export async function openForward(session: string, remotePort: number, label: string, localPort = 0): Promise<Forward | null> {
  try {
    const f = await Colab.addForward(session, remotePort, localPort, label);
    toast("success", `已转发到 ${forwardURL(f)}`, `${label || "端口"} · 虚拟机 ${remotePort} 端口`);
    return f;
  } catch (e) {
    toast("error", "端口转发失败", errorMessage(e));
    return null;
  }
}

export function copyText(text: string) {
  void navigator.clipboard.writeText(text).then(() => toast("success", "已复制", text));
}

export function PortsTab({ session }: { session: string }) {
  const forwards = useForwards(session);
  const [remote, setRemote] = useState("7860");
  const [local, setLocal] = useState("");
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState<number | null>(null);

  async function add(port: number, name: string, localPort = 0) {
    setBusy(port);
    await openForward(session, port, name, localPort);
    setBusy(null);
  }

  const remoteNum = Number(remote);
  const valid = Number.isInteger(remoteNum) && remoteNum > 0 && remoteNum < 65536 && (local === "" || /^\d{1,5}$/.test(local));

  return (
    <div className="h-full overflow-y-auto px-6 py-5">
      <Card className="p-4">
        <div className="text-[13px] font-semibold">把虚拟机上的端口转发到本机</div>
        <div className="mt-1 text-[11px] text-muted-foreground">
          通过 SSH 隧道连接，只监听 127.0.0.1，别的电脑访问不到。会话释放后转发自动停止，不会重新开机。
        </div>
        <div className="mt-3 flex flex-wrap gap-1.5">
          {PRESETS.map((p) => (
            <Button key={p.port} size="sm" loading={busy === p.port} onClick={() => add(p.port, p.label)}>
              {p.label} <span className="font-mono text-[11px] text-muted-foreground">{p.port}</span>
            </Button>
          ))}
        </div>
        <div className="mt-3 flex items-end gap-2">
          <label className="flex flex-col gap-1 text-[11px] text-muted-foreground">
            虚拟机端口
            <input className={cx(inputClass, "w-28 font-mono")} value={remote} onChange={(e) => setRemote(e.target.value.trim())} />
          </label>
          <label className="flex flex-col gap-1 text-[11px] text-muted-foreground">
            本机端口（可空）
            <input className={cx(inputClass, "w-32 font-mono")} value={local} placeholder="同上" onChange={(e) => setLocal(e.target.value.trim())} />
          </label>
          <label className="flex flex-1 flex-col gap-1 text-[11px] text-muted-foreground">
            名称（可空）
            <input className={inputClass} value={label} onChange={(e) => setLabel(e.target.value)} />
          </label>
          <Button variant="accent" icon={<Plus className="size-3.5" />} disabled={!valid} loading={busy === remoteNum} onClick={() => add(remoteNum, label.trim(), local ? Number(local) : 0)}>
            转发
          </Button>
        </div>
      </Card>

      <div className="mt-5">
        {forwards.length === 0 ? (
          <Empty icon={<Cable className="size-5" />} title="还没有转发的端口">
            在虚拟机上启动 Gradio、ComfyUI 等服务后，把它的端口转发到本机，就能在浏览器里打开。
          </Empty>
        ) : (
          <div className="flex flex-col gap-2">
            {forwards.map((f) => (
              <ForwardRow key={f.id} f={f} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

export function ForwardRow({ f }: { f: Forward }) {
  const url = forwardURL(f);
  return (
    <div className="flex items-center gap-3 rounded-xl border border-border bg-card px-3.5 py-2.5">
      <StatusDot tone={f.state === "error" ? "danger" : f.conns > 0 ? "warning" : "success"} pulse={f.conns > 0} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <button className="font-mono text-[13px] font-medium text-accent hover:underline" onClick={() => void Colab.openExternal(url)}>
            {url}
          </button>
          <span className="text-[11px] text-muted-foreground">→ 虚拟机 {f.remotePort}</span>
          {f.label && <Badge>{f.label}</Badge>}
          {f.conns > 0 && <span className="text-[11px] text-muted-foreground">{f.conns} 个连接</span>}
        </div>
        {f.error && <div className="mt-0.5 truncate text-[11px] text-danger">{f.error}</div>}
      </div>
      <IconButton label="复制地址" onClick={() => copyText(url)}>
        <Copy className="size-3.5" />
      </IconButton>
      <IconButton label="在浏览器中打开" onClick={() => void Colab.openExternal(url)}>
        <ExternalLink className="size-3.5" />
      </IconButton>
      <IconButton label="停止转发" onClick={() => void Colab.removeForward(f.id)}>
        <X className="size-3.5" />
      </IconButton>
    </div>
  );
}
