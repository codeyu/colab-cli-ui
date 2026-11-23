import type { Usage } from "./api";

export interface AcceleratorInfo {
  id: string;
  label: string;
  kind: "CPU" | "GPU" | "TPU";
  memory: string;
  vramGB: number; // accelerator memory, for "does the model fit"
  note: string;
  highMem: boolean; // offers a high-RAM shape
}

/** What `colab new --gpu/--tpu` accepts, as the CLI documents it. */
export const ACCELERATORS: AcceleratorInfo[] = [
  { id: "CPU", label: "CPU", kind: "CPU", memory: "—", vramGB: 0, note: "免费层可用，适合轻量任务", highMem: true },
  { id: "T4", label: "T4", kind: "GPU", memory: "16 GB", vramGB: 16, note: "入门 GPU，性价比高", highMem: true },
  { id: "L4", label: "L4", kind: "GPU", memory: "24 GB", vramGB: 24, note: "Ada 架构，推理友好", highMem: false },
  { id: "G4", label: "G4", kind: "GPU", memory: "96 GB", vramGB: 96, note: "RTX PRO 6000 Blackwell", highMem: true },
  { id: "A100", label: "A100", kind: "GPU", memory: "40/80 GB", vramGB: 40, note: "大模型训练", highMem: true },
  { id: "H100", label: "H100", kind: "GPU", memory: "80 GB", vramGB: 80, note: "顶级算力", highMem: true },
  { id: "v5e1", label: "TPU v5e-1", kind: "TPU", memory: "16 GB HBM", vramGB: 16, note: "JAX / PyTorch XLA", highMem: false },
  { id: "v6e1", label: "TPU v6e-1", kind: "TPU", memory: "32 GB HBM", vramGB: 32, note: "Trillium", highMem: false },
];

export type EligibilityState = "eligible" | "ineligible" | "unknown";

/** Whether the account may allocate an accelerator, per the server's ccu-info. */
export function eligibilityOf(usage: Usage | null, id: string): EligibilityState {
  if (id === "CPU") return "eligible";
  const e = usage?.eligibility;
  if (!e?.known) return "unknown";
  const key = id.toUpperCase();
  const has = (l: string[]) => l.some((x) => x.toUpperCase() === key);
  if (has(e.eligibleGpus) || has(e.eligibleTpus)) return "eligible";
  if (has(e.ineligibleGpus) || has(e.ineligibleTpus)) return "ineligible";
  return "unknown";
}

export function acceleratorInfo(id: string): AcceleratorInfo | undefined {
  return ACCELERATORS.find((a) => a.id.toLowerCase() === id.toLowerCase());
}
