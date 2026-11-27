import { Colab } from "./api";

/** Tasks shorter than this end without a notification. */
const LONG_MS = 60_000;

/**
 * Shows a desktop notification that a long task ended, unless the user is
 * looking at the window. started is when the task began (ms).
 */
export function notifyDone(title: string, body: string, started: number) {
  if (Date.now() - started < LONG_MS) return;
  if (document.visibilityState === "visible" && document.hasFocus()) return;
  void Colab.notify(title, body).catch(() => {});
}
