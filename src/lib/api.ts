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
 */
export async function streamCall(
  start: (ch: Channel<Chunk>) => Promise<void>,
  onChunk: (c: Chunk) => void,
): Promise<void> {
  const ch = new Channel<Chunk>(onChunk);
  await start(ch);
}
