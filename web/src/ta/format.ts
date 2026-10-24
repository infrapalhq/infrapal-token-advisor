// Display formatting shared by the Token Advisor pages.

export function money(n: number): string {
  return `$${n.toFixed(2)}`;
}

// tokensHuman: 512 -> "512", 31_000 -> "31K", 15_200_000 -> "15.2M".
export function tokensHuman(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1000) return `${Math.round(n / 1000)}K`;
  return `${Math.round(n)}`;
}

// durationS: 3120 -> "52m", 3900 -> "1h 05m", 240 -> "4m".
export function durationS(s: number): string {
  if (!s) return "—";
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`;
  return `${Math.max(m, 1)}m`;
}

// waitMs: 0 -> "—", 40_000 -> "40s", 75_000 -> "1m 15s".
export function waitMs(ms: number): string {
  const sec = Math.round(ms / 1000);
  if (sec === 0) return "—";
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60);
  const r = sec % 60;
  return r ? `${m}m ${r}s` : `${m}m`;
}

export function dayShort(ms: number): string {
  return new Date(ms).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

export function timeHM(ms: number): string {
  return new Date(ms).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
}

export const AREA_LABEL: Record<string, string> = {
  model: "Model",
  context: "Context",
  exec: "Execution",
  perm: "Permissions",
};

export const AREA_NAME: Record<string, string> = {
  model: "Model–task mismatch",
  context: "Context health",
  exec: "Wasteful agent execution",
  perm: "Permission friction",
};

export const SYMPTOM_LABEL: Record<string, string> = {
  compacted: "Compacted",
  failed: "Failed tool calls",
  waits: "Long approval waits",
  repeats: "Repeated reads or commands",
};

export function pipClass(sev: string | undefined): string {
  return sev === "full" ? "full" : sev === "half" ? "half" : "";
}
