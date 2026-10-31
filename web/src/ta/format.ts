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
  bloated: "Bloated context",
  many_calls: "Many tool calls",
  spikes: "Large tool results",
  rejected: "Rejected tool calls",
};

export function pipClass(sev: string | undefined): string {
  return sev === "full" ? "full" : sev === "half" ? "half" : "";
}

export function sessionCount(n: number): string {
  return `${n} session${n === 1 ? "" : "s"}`;
}

// itemSessions labels how widely a fix applies. An item with no sessions belongs
// to its scope if it has one, such as a project, and otherwise to the install,
// such as an MCP server that loads into every session.
export function itemSessions(n: number, scope?: string): string {
  return n === 0 ? scope || "All sessions" : sessionCount(n);
}

// sessionTime is how long a session was worked on. Open time is only a fallback,
// since a window left open for days says nothing about the work.
export function sessionTime(s: { active_s: number; duration_s: number }): string {
  return s.active_s > 0 ? `${durationS(s.active_s)} active` : durationS(s.duration_s);
}

// isEmptySession mirrors the API's rule: nothing billed, nothing flagged.
export function isEmptySession(s: { tokens: number; issues: number }): boolean {
  return s.tokens === 0 && s.issues === 0;
}

// areaCounts is an Overview problem card's line: session issues and setup issues
// are counted apart, since setup issues belong to no session.
export function areaCounts(issues: number, sessions: number, setup: number): string {
  const parts: string[] = [];
  if (issues > 0) parts.push(`${issues} issue${issues === 1 ? "" : "s"} · ${sessionCount(sessions)}`);
  if (setup > 0) parts.push(`${setup} setup issue${setup === 1 ? "" : "s"}`);
  return parts.join(" · ") || "No issues";
}

export function areaLink(sessions: number, setup: number): string {
  return sessions === 0 && setup > 0 ? "See fixes" : sessionCount(sessions);
}
