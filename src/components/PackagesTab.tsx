import { useState } from "react";
import { FileText, Package } from "lucide-react";
import { Colab } from "../lib/api";
import { cellsStore, useStore } from "../lib/store";
import { runCell } from "./ConsoleTab";
import { Output } from "./Output";
import { Button, Empty, Spinner, inputClass } from "./ui";

const PRESETS = [
  { label: "Transformers 全家桶", pkgs: "transformers accelerate datasets peft" },
  { label: "可视化", pkgs: "matplotlib seaborn plotly" },
  { label: "数据处理", pkgs: "pandas polars pyarrow" },
  { label: "推理加速", pkgs: "vllm" },
  { label: "Gradio", pkgs: "gradio" },
];

export function PackagesTab({ session }: { session: string }) {
  const [pkgs, setPkgs] = useState("");
  const cells = (useStore(cellsStore)[session] ?? []).filter((c) => c.kind === "install");
  const running = cells.some((c) => c.status === "running");

  function install(list: string[], requirements = "") {
    void runCell(
      session,
      { kind: "install", title: requirements ? `pip install -r ${requirements.split(/[\\/]/).pop()}` : `pip install ${list.join(" ")}` },
      (signal, ch) => Colab.install.with({ signal })(session, list, requirements, ch),
    );
  }

  async function fromRequirements() {
    const f = await Colab.pickFile("requirements");
    if (f) install([], f);
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-b border-border px-6 py-4">
        <div className="mx-auto max-w-[980px]">
          <div className="text-[13px] font-medium">安装 Python 包</div>
          <div className="mt-0.5 text-xs text-muted-foreground">
            通过 <code className="font-mono">colab install</code> 在虚拟机上用 uv 安装（失败时回退到 pip）。多个包用空格分隔，可写版本约束。
          </div>
          <form
            className="mt-3 flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              const list = pkgs.split(/\s+/).filter(Boolean);
              if (list.length) install(list);
            }}
          >
            <input className={inputClass} value={pkgs} onChange={(e) => setPkgs(e.target.value)} placeholder="例如：transformers==4.48.0 bitsandbytes" spellCheck={false} />
            <Button type="submit" variant="accent" icon={<Package className="size-3.5" />} disabled={!pkgs.trim()}>
              安装
            </Button>
            <Button type="button" icon={<FileText className="size-3.5" />} onClick={fromRequirements}>
              requirements.txt
            </Button>
          </form>
          <div className="mt-2.5 flex flex-wrap gap-1.5">
            {PRESETS.map((p) => (
              <button
                key={p.label}
                onClick={() => setPkgs(p.pkgs)}
                className="rounded-md border border-border px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
                title={p.pkgs}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {cells.length === 0 ? (
          <Empty icon={<Package className="size-5" />} title="还没有安装记录">
            Colab 运行时已预装 PyTorch、NumPy 等常用库，可以先在控制台用 <code className="font-mono">pip list</code> 查看。
          </Empty>
        ) : (
          <div className="mx-auto flex max-w-[980px] flex-col gap-3 px-6 py-5">
            {[...cells].reverse().map((c) => (
              <div key={c.id} className="rounded-xl border border-border bg-card">
                <div className="flex items-center gap-2 border-b border-border px-3 py-1.5 text-xs font-medium">
                  {c.status === "running" && <Spinner className="text-accent" />}
                  <span className="font-mono">{c.title}</span>
                  <span className="ml-auto text-[11px] text-muted-foreground">
                    {c.status === "running" ? "安装中…" : c.status === "done" ? "完成" : c.status === "canceled" ? "已取消" : "失败"}
                  </span>
                  {c.status === "running" && (
                    <Button size="sm" variant="ghost" className="h-6" onClick={() => c.abort?.abort()}>
                      停止
                    </Button>
                  )}
                </div>
                <div className="max-h-80 overflow-auto px-3.5 py-2.5">
                  <Output chunks={c.chunks} hideInfo />
                  {c.error && <div className="ansi-out mt-1 text-danger">{c.error}</div>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
      {running && <div className="border-t border-border px-6 py-1.5 text-[11px] text-muted-foreground">安装过程中内核会被占用，其它执行会排队。</div>}
    </div>
  );
}
