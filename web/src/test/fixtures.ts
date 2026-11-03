import type { AgentInfo, AiConfig, Fixes, Item, McpSetup, Overview, Progress, SessionCard, SessionCards, Status, Timeline } from "../ta/api";

// Sample API answers for the tests. Each builder takes overrides, so a test
// states only what it's about.

export const DAY = 24 * 60 * 60 * 1000;
export const T0 = Date.parse("2026-10-20T09:00:00Z");

export const status = (o: Partial<Status> = {}): Status => ({ collector_live: true, prompts_logged_7d: true, last_event_ms: T0, ...o });

export const agent = (o: Partial<AgentInfo> = {}): AgentInfo => ({ id: "claude-code", name: "Claude Code", sessions: 3, cost_known: true, skipped: [], ...o });

export const aiConfig = (o: Partial<AiConfig> = {}): AiConfig => ({
  configured: false,
  provider: "anthropic",
  provider_name: "Anthropic",
  key_hint: "sk-ant-…",
  providers: [{ id: "anthropic", name: "Anthropic" }],
  ...o,
});

export const item = (o: Partial<Item> = {}): Item => ({
  key: "reread|src/a.ts",
  kind: "reread",
  area: "context",
  subject: "src/a.ts",
  title: "Re-read src/a.ts 6 times",
  detail: "The same file was read again and again.",
  severity: "full",
  conf: "Measured",
  occurrences: 6,
  sessions: 2,
  usd: 1.25,
  fix: "Read src/a.ts once.",
  fix_lang: "claude-md",
  score: 10,
  last_ts: T0,
  marked: false,
  ...o,
});

export const card = (o: Partial<SessionCard> = {}): SessionCard => ({
  id: "s1",
  project: "api",
  title: "Fix the login bug",
  started_at: T0,
  duration_s: 3900,
  tokens: 31_000,
  cost_usd: 2.5,
  wait_ms: 75_000,
  model: "claude-opus-5-5",
  areas: [{ area: "context", severity: "full" }],
  priority: "now",
  is_new: true,
  symptoms: ["repeats"],
  issues: 1,
  ...o,
});

export const cards = (sessions: SessionCard[]): SessionCards => ({
  range: "7d",
  sessions,
  count_all: sessions.length,
  count_attention: sessions.filter((s) => s.priority !== "fine").length,
  count_new: sessions.filter((s) => s.is_new).length,
});

export const overview = (o: Partial<Overview> = {}): Overview => ({
  range: "7d",
  spend_usd: 12.5,
  tokens: 1_500_000,
  cache_share: 0.62,
  waiting_ms: 75_000,
  idle_ms: 0,
  avoidable_usd: 3.4,
  recovered: { usd: 2.1, minutes: 4 },
  fix_first: [item(), item({ key: "wait|git", area: "perm", title: "Approved git status 9 times", usd: undefined, minutes: 3.4, sessions: 1, severity: "half" })],
  areas: [
    { area: "context", items: 2, usd: 3.4, severity: "full", sessions: 2 },
    { area: "perm", items: 1, usd: 0, severity: "fine", sessions: 1 },
  ],
  by_day: [
    { day: "2026-10-19", cost_usd: 4, avoidable_usd: 1 },
    { day: "2026-10-20", cost_usd: 8.5, avoidable_usd: 2.4 },
  ],
  by_project: [{ project: "api", cost_usd: 12.5, sessions: 3 }],
  models: [{ model: "claude-opus-5-5", cost_usd: 12.5, share: 1 }],
  review: [card()],
  sessions_count: 3,
  cost_unknown_sessions: 0,
  ...o,
});

export const fixes = (o: Partial<Fixes> = {}): Fixes => {
  const f = {
    range: "all",
    to_fix: [item()],
    habits: [item({ key: "habit|x", title: "Start fresh sessions more often", fix: undefined, note: "Long sessions re-send old context." })],
    marked: [item({ key: "allow|ls", title: "Allow ls", marked: true, marked_at: Date.now() - DAY }), item({ key: "allow|cat", title: "Allow cat", marked: true, marked_at: Date.now() - 30 * DAY })],
    ...o,
  };
  return { ...f, counts: { to_fix: f.to_fix.length, habits: f.habits.length, marked: f.marked.length } };
};

export const progress = (weeks: Progress["weeks"] = [
  { week: "2026-10-05", avoidable_usd: 10, sessions: 4, healthy_pct: 0.5, marked_fixes: 0 },
  { week: "2026-10-12", avoidable_usd: 4, sessions: 5, healthy_pct: 0.8, marked_fixes: 2 },
]): Progress => ({ range: "install", weeks });

export const mcpSetup = (o: Partial<McpSetup> = {}): McpSetup => ({
  server_name: "infrapal-token-advisor",
  agent_name: "Claude Code",
  enabled: false,
  command: "claude mcp add infrapal-token-advisor",
  verify: "claude mcp list",
  remove: "claude mcp remove infrapal-token-advisor",
  ...o,
});

export const timeline = (o: Partial<Timeline> = {}): Timeline => ({
  session_id: "s1",
  started_at: T0,
  peak_context: 40_000,
  compactions: 1,
  agent_name: "Claude Code",
  cost_unknown: false,
  rows: [
    { id: "d1", kind: "div", ts: T0, text: "PROMPT 1", meta: "09:00 · $1.00", prompt_id: "p1" },
    { id: "u1", kind: "you", ts: T0, text: "Fix the login bug", prompt_id: "p1" },
    { id: "t1", kind: "tool", ts: T0 + 1, name: "Read", text: "src/a.ts", context_tokens: 20_000, meta: "0.2s" },
    { id: "t2", kind: "tool", ts: T0 + 2, name: "Bash", text: "git status", context_tokens: 40_000, wait_ms: 12_000 },
    { id: "c1", kind: "claude", ts: T0 + 3, text: "Done." },
    { id: "e1", kind: "event", ts: T0 + 4, text: "Compacted" },
    { id: "u2", kind: "you", ts: T0 + 5, text: "", prompt_id: "p2" },
  ],
  receipt: {
    cost_usd: 2.5,
    avoidable_usd: 0.75,
    well_spent: 0.7,
    calls: 4,
    tokens: { fresh_input: 10_000, output: 2_000, cache_read: 18_000, cache_write: 1_000 },
    by_area_usd: { context: 0.75, perm: 0 },
    prompts: [
      { prompt_id: "p1", label: "Fix the login bug", cost_usd: 2, tokens: 25_000, flags: 1 },
      { prompt_id: "p2", label: "", cost_usd: 0.5, tokens: 6_000, flags: 0 },
    ],
  },
  ...o,
});

// base answers the calls every page's sidebar makes.
export const base = () => ({
  "/api/status": status(),
  "/api/agents": { agents: [agent()] },
  "/api/ai/config": aiConfig(),
  "POST /api/ui/visit": { last_visit_at: T0, previous_visit_at: T0 - DAY },
});
