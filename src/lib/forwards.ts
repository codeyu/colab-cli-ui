import { Colab, events, type Forward } from "./api";
import { createStore, useStore } from "./store";

/** The forwarded ports, kept current from the Go side. */
export const forwardsStore = createStore<Forward[]>([]);

let started = false;
function start() {
  if (started) return;
  started = true;
  events.forwardsChanged.on((l) => forwardsStore.set(l));
  void Colab.listForwards().then((l) => forwardsStore.set(l));
}

export function useForwards(session?: string): Forward[] {
  start();
  const all = useStore(forwardsStore);
  return session ? all.filter((f) => f.session === session) : all;
}

export function forwardURL(f: Forward): string {
  return `http://127.0.0.1:${f.localPort}`;
}
