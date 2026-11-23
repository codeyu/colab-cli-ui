import { useCallback, useEffect, useState } from "react";
import { onFileDrop } from "mygo-runtime";
import {
  ArrowUp,
  ChevronRight,
  Copy,
  Download,
  File,
  FileCode2,
  FileImage,
  FileText,
  Folder,
  HardDrive,
  RefreshCw,
  Trash2,
  Upload,
} from "lucide-react";
import { Colab, errorMessage, type FileEntry } from "../lib/api";
import { cx } from "../lib/format";
import { toast } from "../lib/store";
import { Button, Dialog, Empty, IconButton, Spinner } from "./ui";

const lastDir: Record<string, string> = {};

function fileIcon(name: string) {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  if (["py", "ipynb", "js", "ts", "sh", "json", "yaml", "yml", "toml"].includes(ext)) return <FileCode2 className="size-4 text-info" />;
  if (["png", "jpg", "jpeg", "gif", "webp", "svg"].includes(ext)) return <FileImage className="size-4 text-success" />;
  if (["txt", "md", "csv", "log"].includes(ext)) return <FileText className="size-4 text-muted-foreground" />;
  return <File className="size-4 text-muted-foreground" />;
}

export function FilesTab({ session }: { session: string }) {
  const [dir, setDir] = useState(lastDir[session] ?? "content");
  const [entries, setEntries] = useState<FileEntry[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busyPath, setBusyPath] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const [confirm, setConfirm] = useState<FileEntry | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  const load = useCallback(
    async (d: string) => {
      setLoading(true);
      setError(null);
      try {
        const list = await Colab.listFiles(session, d);
        setEntries(list);
        setDir(d);
        lastDir[session] = d;
      } catch (e) {
        setError(errorMessage(e));
      } finally {
        setLoading(false);
      }
    },
    [session],
  );

  useEffect(() => {
    setEntries(null);
    void load(lastDir[session] ?? "content");
  }, [session, load]);

  const uploadPaths = useCallback(
    async (paths: string[] | null) => {
      setUploading(true);
      try {
        const done = paths ? await Colab.uploadPaths(session, dir, paths) : await Colab.upload(session, dir);
        if (done.length) {
          toast("success", `已上传 ${done.length} 个文件`, done.join("\n"));
          await load(dir);
        }
      } catch (e) {
        toast("error", "上传失败", errorMessage(e));
      } finally {
        setUploading(false);
      }
    },
    [session, dir, load],
  );

  useEffect(
    () =>
      onFileDrop(({ paths }) => {
        setDragOver(false);
        if (paths.length) void uploadPaths(paths);
      }),
    [uploadPaths],
  );

  async function download(e: FileEntry) {
    setBusyPath(e.path);
    try {
      const local = await Colab.download(session, e.path);
      if (local) toast("success", "下载完成", local);
    } catch (err) {
      toast("error", "下载失败", errorMessage(err));
    } finally {
      setBusyPath(null);
    }
  }

  async function remove(e: FileEntry) {
    setConfirm(null);
    setBusyPath(e.path);
    try {
      await Colab.removeFile(session, e.path);
      toast("success", "已删除", "/" + e.path);
      await load(dir);
    } catch (err) {
      toast("error", "删除失败", errorMessage(err));
    } finally {
      setBusyPath(null);
    }
  }

  const parts = dir ? dir.split("/") : [];
  const up = parts.slice(0, -1).join("/");

  return (
    <div
      className="relative flex h-full min-h-0 flex-col"
      onDragOver={(e) => {
        e.preventDefault();
        setDragOver(true);
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setDragOver(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
      }}
    >
      <div className="flex items-center gap-1 border-b border-border px-4 py-2">
        <IconButton label="上一级" disabled={!dir || loading} onClick={() => load(up)}>
          <ArrowUp className="size-4" />
        </IconButton>
        <IconButton label="刷新" disabled={loading} onClick={() => load(dir)}>
          {loading ? <Spinner /> : <RefreshCw className="size-3.5" />}
        </IconButton>
        <div className="ml-1 flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto text-[13px]">
          <button className="flex items-center gap-1 rounded px-1.5 py-0.5 text-muted-foreground hover:bg-muted hover:text-foreground" onClick={() => load("")}>
            <HardDrive className="size-3.5" />/
          </button>
          {parts.map((p, i) => (
            <span key={i} className="flex items-center gap-0.5">
              {i > 0 && <ChevronRight className="size-3 text-muted-foreground" />}
              <button
                className={cx("rounded px-1.5 py-0.5 hover:bg-muted", i === parts.length - 1 ? "font-medium" : "text-muted-foreground")}
                onClick={() => load(parts.slice(0, i + 1).join("/"))}
              >
                {p}
              </button>
            </span>
          ))}
        </div>
        <Button size="sm" variant="accent" loading={uploading} icon={<Upload className="size-3.5" />} onClick={() => uploadPaths(null)}>
          上传文件
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {error && <div className="m-4 rounded-lg border border-danger/30 bg-danger/8 p-3 text-xs break-words text-danger">{error}</div>}
        {entries === null && !error && (
          <div className="flex items-center gap-2 p-4 text-xs text-muted-foreground">
            <Spinner /> 正在读取目录…
          </div>
        )}
        {entries?.length === 0 && (
          <Empty icon={<Folder className="size-5" />} title="空目录">
            把文件拖到这里，或点击「上传文件」。
          </Empty>
        )}
        {entries && entries.length > 0 && (
          <div className="px-2 py-1.5">
            {entries.map((e) => (
              <div
                key={e.path}
                onClick={() => setSelected(e.path)}
                onDoubleClick={() => (e.isDir ? load(e.path) : download(e))}
                className={cx(
                  "group flex h-8 cursor-default items-center gap-2.5 rounded-lg px-2.5",
                  selected === e.path ? "bg-muted" : "hover:bg-muted/60",
                )}
              >
                {e.isDir ? <Folder className="size-4 text-accent" /> : fileIcon(e.name)}
                <span className="min-w-0 flex-1 truncate text-[13px]">{e.name}</span>
                {busyPath === e.path && <Spinner />}
                <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                  <IconButton label="复制路径" className="size-6" onClick={() => navigator.clipboard.writeText("/" + e.path)}>
                    <Copy className="size-3" />
                  </IconButton>
                  {!e.isDir && (
                    <IconButton label="下载" className="size-6" onClick={() => download(e)}>
                      <Download className="size-3.5" />
                    </IconButton>
                  )}
                  <IconButton label="删除" className="size-6 hover:text-danger" onClick={() => setConfirm(e)}>
                    <Trash2 className="size-3.5" />
                  </IconButton>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="border-t border-border px-4 py-1.5 text-[11px] text-muted-foreground">
        {entries ? `${entries.filter((e) => e.isDir).length} 个文件夹，${entries.filter((e) => !e.isDir).length} 个文件` : ""}
        <span className="float-right">双击文件夹打开 · 双击文件下载 · 拖入文件上传到当前目录</span>
      </div>

      {dragOver && (
        <div className="pointer-events-none absolute inset-2 flex items-center justify-center rounded-xl border-2 border-dashed border-accent bg-accent-soft text-sm font-medium text-accent">
          松开即可上传到 /{dir}
        </div>
      )}

      <Dialog
        open={confirm !== null}
        onClose={() => setConfirm(null)}
        title="删除远程文件？"
        description={confirm ? `/${confirm.path} 将从虚拟机上删除，无法恢复。` : ""}
        width={420}
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirm(null)}>
              取消
            </Button>
            <Button variant="danger" onClick={() => confirm && remove(confirm)}>
              删除
            </Button>
          </>
        }
      >
        <div />
      </Dialog>
    </div>
  );
}
