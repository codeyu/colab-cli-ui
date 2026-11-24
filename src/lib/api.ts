import { Channel, isCallError } from "mygo-runtime";
import type { Chunk } from "../mygo";

export { Colab, events } from "../mygo";
export type * from "../mygo";

/** The message of an error from Go or anything else thrown. */
export function errorMessage(err: unknown): string {
  if (isCallError(err)) return err.message;
  if (err instanceof DOMException && err.name === "AbortError") return "已取消";
  if (err instanceof Error) return err.message;
  return String(err);
}

export function isAbort(err: unknown): boolean {
  return (
    (err instanceof DOMException && err.name === "AbortError") ||
    (err instanceof Error && err.message === "已取消")
  );
}

/**
 * Runs a streaming call: `start` gets a channel to pass to the Go method and
 * every chunk it sends reaches `onChunk`. Resolves when the method returns.
 *
 * `signal` cancels it by closing the channel, which cancels the method's
 * context, and rejects at once with an AbortError while the method winds
 * down. Do not pass a signal to the call itself (`.with({ signal })`): in
 * MyGo 0.10 a call that may be cancelled gets its channels already closed,
 * so the method cannot send anything.
 */
export function streamCall(start: (ch: Channel<Chunk>) => Promise<void>, onChunk: (c: Chunk) => void, signal?: AbortSignal): Promise<void> {
  const ch = new Channel<Chunk>(onChunk);
  const call = start(ch);
  if (!signal) return call;
  if (signal.aborted) {
    ch.close();
    call.catch(() => {});
    return Promise.reject(new DOMException("aborted", "AbortError"));
  }
  return new Promise<void>((resolve, reject) => {
    const onAbort = () => {
      ch.close();
      reject(new DOMException("aborted", "AbortError"));
    };
    signal.addEventListener("abort", onAbort, { once: true });
    call.then(
      () => {
        signal.removeEventListener("abort", onAbort);
        resolve();
      },
      (e) => {
        signal.removeEventListener("abort", onAbort);
        reject(e);
      },
    );
  });
}
