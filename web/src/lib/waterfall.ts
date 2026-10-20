import type { ApiRequest, Interaction, ToolCall } from "./api";

export type RowKind = "llm" | "tool" | "mcp" | "subagent";

// minPerceptibleBlockedMs is the floor below which blocked_on_user_ms is
// treated as approval overhead noise rather than a real wait — a handful
// of milliseconds from a config-decided auto-approval, not a pause for a
// human. Below this, the hatch overlay doesn't render at all: at a
// zoomed-out timeline scale, a hatch's own minimum render width would
// otherwise make a near-instant approval look like the whole call was
// blocked.
const minPerceptibleBlockedMs = 250;

export type WaterfallRow = {
  id: string;
  kind: RowKind;
  label: string;
  sublabel: string | null;
  startMs: number;
  endMs: number;
  blockedStartMs: number | null;
  blockedEndMs: number | null;
  depth: number;
  toolCall: ToolCall | null;
  apiRequest: ApiRequest | null;
};

// buildWaterfallRows turns one interaction's flat api_requests + tool_calls
// arrays into a single time-sorted row list the waterfall renders.
//
// Simplification (documented in HANDOFF.md "Milestone 6 recap"): the API
// does not expose a clean span_id <-> tool_use_id join for every span type,
// so rows are built directly from tool_calls/api_requests (already the
// richest merged view of each call) rather than walking the raw span tree.
// Subagent nesting is approximated by tool_calls.subagent_type: a call
// carrying one renders one level indented, since that is the strongest
// available "this ran inside a subagent" signal.
export function buildWaterfallRows(
  interaction: Interaction,
  apiRequests: ApiRequest[],
  toolCalls: ToolCall[],
): WaterfallRow[] {
  const originMs = interaction.started_at;

  const llmRows: WaterfallRow[] = apiRequests.map((req) => {
    const startMs = req.ts - originMs;
    return {
      id: `req-${req.request_id}`,
      kind: "llm",
      label: req.model ?? "LLM call",
      sublabel: req.stop_reason,
      startMs,
      endMs: startMs + (req.duration_ms ?? 0),
      blockedStartMs: null,
      blockedEndMs: null,
      depth: 0,
      toolCall: null,
      apiRequest: req,
    };
  });

  const toolRows: WaterfallRow[] = toolCalls.map((tc) => {
    const startMs = tc.ts - originMs;
    const endMs = startMs + (tc.duration_ms ?? 0);
    const kind: RowKind = tc.mcp_server_name
      ? "mcp"
      : tc.subagent_type
        ? "subagent"
        : "tool";
    const sublabel = tc.mcp_server_name
      ? `mcp: ${tc.mcp_server_name}`
      : tc.subagent_type
        ? `subagent: ${tc.subagent_type}`
        : (tc.full_command ?? null);
    const blocked = tc.blocked_on_user_ms != null && tc.blocked_on_user_ms >= minPerceptibleBlockedMs;
    return {
      id: `tool-${tc.tool_use_id}`,
      kind,
      label: tc.mcp_tool_name ?? tc.tool_name ?? "tool",
      sublabel,
      startMs,
      endMs,
      blockedStartMs: blocked ? startMs : null,
      blockedEndMs: blocked ? startMs + tc.blocked_on_user_ms! : null,
      depth: tc.subagent_type ? 1 : 0,
      toolCall: tc,
      apiRequest: null,
    };
  });

  return [...llmRows, ...toolRows].sort((a, b) => a.startMs - b.startMs);
}

export function totalDurationMs(
  interaction: Interaction,
  rows: WaterfallRow[],
): number {
  const fromRows = rows.reduce((max, r) => Math.max(max, r.endMs), 0);
  return Math.max(interaction.duration_ms ?? 0, fromRows, 100);
}

export function rowColorClass(kind: RowKind): string {
  switch (kind) {
    case "llm":
      return "bg-cat-llm";
    case "mcp":
      return "bg-cat-mcp";
    case "subagent":
      return "bg-cat-subagent";
    case "tool":
    default:
      return "bg-cat-tool";
  }
}

// niceTickIntervalMs picks a human-friendly tick spacing (in ms) so a ruler
// over totalMs gets somewhere between 4 and 10 ticks.
export function niceTickIntervalMs(totalMs: number): number {
  const steps = [50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 300000];
  for (const step of steps) {
    if (totalMs / step <= 10) return step;
  }
  return Math.ceil(totalMs / 10 / 60000) * 60000;
}
