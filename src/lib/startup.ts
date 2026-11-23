import { Colab, errorMessage, type HistoryEvent } from "./api";
import { toast } from "./store";
import { runCell } from "../components/ConsoleTab";

/** Code the app runs for itself, which replaying should leave out. */
const INTERNAL = "__COLAB_UI_STATS__";

/** Whether a history entry is code that can run again. */
export function replayable(e: HistoryEvent): boolean {
  if (!e.code || e.code.includes(INTERNAL)) return false;
  return e.type === "execution" || (e.type === "automation" && e.fields?.op === "install");
}

/** Whether a session's history mounted Google Drive. */
export function needsDrive(events: HistoryEvent[]): boolean {
  return events.some((e) => e.type === "automation" && e.fields?.op === "drivemount");
}

async function driveMounted(name: string): Promise<boolean> {
  try {
    return (await Colab.listFiles(name, "content/drive")).some((f) => f.name === "MyDrive");
  } catch {
    return false;
  }
}

/**
 * Makes sure Drive is mounted on the session, opening the sign-in terminal if
 * not. It checks with the Jupyter contents API, which does not wait for the
 * kernel, so the mount (which runs in the kernel) is not held up by it.
 */
export async function waitForDrive(name: string): Promise<boolean> {
  if (await driveMounted(name)) return true;
  try {
    await Colab.openTerminal(name, "drivemount");
  } catch (e) {
    toast("error", "打开终端失败", errorMessage(e));
    return false;
  }
  toast("info", "请在弹出的终端里完成 Drive 授权", "挂载完成后自动继续执行");
  const deadline = Date.now() + 10 * 60_000;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 5000));
    if (await driveMounted(name)) return true;
  }
  toast("error", "10 分钟内没有挂载 Drive，已取消执行");
  return false;
}

/**
 * Runs code steps one after another in a session's console, mounting Drive
 * first when asked. label names the cells, e.g. a startup script's name.
 */
export async function runSteps(target: string, codes: string[], opts: { label: string; stopOnError?: boolean; drive?: boolean }) {
  const { label, stopOnError = true, drive = false } = opts;
  if (drive && !(await waitForDrive(target))) return;
  let failed = 0;
  for (const [i, code] of codes.entries()) {
    const title = codes.length === 1 ? label : `${label} ${i + 1}/${codes.length}`;
    const ok = await runCell(target, { kind: "python", title, code }, (signal, ch) => Colab.exec.with({ signal })(target, code, false, ch));
    if (!ok) {
      failed++;
      if (stopOnError) {
        toast("error", `${label}：第 ${i + 1} 步失败，已停止`);
        return;
      }
    }
  }
  toast(failed ? "error" : "success", failed ? `${label}：${failed} 步失败` : `${label}：完成`);
}

