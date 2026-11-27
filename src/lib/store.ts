import { useSyncExternalStore } from "react";
import type { ActivityEntry, Chunk } from "./api";

/** A tiny observable value, shared by components without a provider. */
export function createStore<T>(initial: T) {
  let value = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    set(next: T | ((prev: T) => T)) {
      value = typeof next === "function" ? (next as (p: T) => T)(value) : next;
      for (const l of listeners) l();
    },
    subscribe(l: () => void) {
      listeners.add(l);
      return () => listeners.delete(l);
    },
  };
}

export type Store<T> = ReturnType<typeof createStore<T>>;

export function useStore<T>(store: Store<T>): T {
  return useSyncExternalStore(store.subscribe, store.get, store.get);
}

// ---- Console cells, kept per session while the app runs ----

export type CellStatus = "running" | "done" | "error" | "canceled";

export interface Cell {
  id: number;
  kind: "python" | "shell" | "file" | "install" | "create" | "run" | "download" | "deploy" | "task";
  title: string;
  code?: string;
  chunks: Chunk[];
  status: CellStatus;
  error?: string;
  started: number;
  ended?: number;
  abort?: AbortController;
}

export const cellsStore = createStore<Record<string, Cell[]>>({});
let cellSeq = 0;

export function addCell(session: string, cell: Omit<Cell, "id" | "chunks" | "status" | "started">): number {
  const id = ++cellSeq;
  cellsStore.set((all) => ({
    ...all,
    [session]: [...(all[session] ?? []), { ...cell, id, chunks: [], status: "running", started: Date.now() }],
  }));
  return id;
}

export function updateCell(session: string, id: number, fn: (c: Cell) => Cell) {
  cellsStore.set((all) => ({
    ...all,
    [session]: (all[session] ?? []).map((c) => (c.id === id ? fn(c) : c)),
  }));
}

/** Appends output, merging consecutive text of the same stream. */
export function appendChunk(session: string, id: number, chunk: Chunk) {
  updateCell(session, id, (c) => {
    const last = c.chunks[c.chunks.length - 1];
    if (last && chunk.stream !== "image" && last.stream === chunk.stream && (last.text?.length ?? 0) < 200_000) {
      return { ...c, chunks: [...c.chunks.slice(0, -1), { ...last, text: (last.text ?? "") + (chunk.text ?? "") }] };
    }
    return { ...c, chunks: [...c.chunks, chunk] };
  });
}

export function clearCells(session: string) {
  cellsStore.set((all) => ({ ...all, [session]: (all[session] ?? []).filter((c) => c.status === "running") }));
}

export function runningCells(session: string): Cell[] {
  return (cellsStore.get()[session] ?? []).filter((c) => c.status === "running");
}

// ---- CLI activity log ----

export const activityStore = createStore<ActivityEntry[]>([]);

export function recordActivity(e: ActivityEntry) {
  activityStore.set((list) => {
    const i = list.findIndex((x) => x.id === e.id);
    const next = i >= 0 ? list.map((x) => (x.id === e.id ? e : x)) : [...list, e];
    return next.length > 300 ? next.slice(-300) : next;
  });
}

// ---- Toasts ----

export interface Toast {
  id: number;
  kind: "success" | "error" | "info";
  title: string;
  detail?: string;
}

export const toastStore = createStore<Toast[]>([]);
let toastSeq = 0;

export function toast(kind: Toast["kind"], title: string, detail?: string) {
  const id = ++toastSeq;
  toastStore.set((l) => [...l, { id, kind, title, detail }]);
  setTimeout(() => toastStore.set((l) => l.filter((t) => t.id !== id)), kind === "error" ? 8000 : 4000);
}

export function dismissToast(id: number) {
  toastStore.set((l) => l.filter((t) => t.id !== id));
}
