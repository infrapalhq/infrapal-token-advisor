// Typed client for the Token Advisor endpoints (internal/api/insight_*.go).
// Every request goes to the same origin; Vite proxies /api in dev, and the
// Go binary serves both in production.

export type Range = "today" | "7d" | "30d" | "3m" | "install" | "all";

export type Priority = "now" | "look" | "fine";
export type Severity = "full" | "half";
export type Conf = "Measured" | "Likely" | "Estimated";

export type AreaPip = { area: "model" | "context" | "exec" | "perm"; severity: Severity };

export type SessionCard = {
  id: string;
  project: string;
  title: string;
  started_at: number;
  duration_s: number;
  tokens: number;
  cost_usd: number;
  wait_ms: number;
  model: string;
  areas: AreaPip[];
  priority: Priority;
  is_new: boolean;
  symptoms: ("compacted" | "failed" | "waits" | "repeats")[];
  issues: number;
};

export type SessionCards = {
  range: string;
  sessions: SessionCard[];
  count_all: number;
  count_attention: number;
  count_new: number;
};

export type Item = {
  key: string;
  kind: string;
  area: "model" | "context" | "exec" | "perm";
  subject: string;
  title: string;
  detail: string;
  severity: Severity;
  conf: Conf;
  occurrences: number;
  sessions: number;
  tokens?: number;
  usd?: number;
  minutes?: number;
  fix?: string;
  fix_lang?: "bash" | "settings-json" | "claude-md";
  note?: string;
  score: number;
  last_ts: number;
  marked: boolean;
  marked_at?: number;
};

export type Overview = {
  range: string;
  spend_usd: number;
  tokens: number;
  cache_share: number;
  waiting_ms: number;
  idle_ms: number;
  avoidable_usd: number;
  recovered: { usd: number; minutes: number };
  fix_first: Item[];
  areas: { area: string; items: number; usd: number; severity: string; sessions: number }[];
  by_day: { day: string; cost_usd: number; avoidable_usd: number }[];
  by_project: { project: string; cost_usd: number; sessions: number }[];
  models: { model: string; cost_usd: number; share: number }[];
  review: SessionCard[];
  sessions_count: number;
};

export type Fixes = {
  range: string;
  to_fix: Item[];
  habits: Item[];
  marked: Item[];
  counts: { to_fix: number; habits: number; marked: number };
};

export type TimelineRow = {
  id: string;
  kind: "div" | "you" | "claude" | "tool" | "event";
  ts: number;
  text: string;
  name?: string;
  meta?: string;
  prompt_id?: string;
  context_tokens?: number;
  wait_ms?: number;
  flag?: string;
  flag_label?: string;
};

export type Occurrence = {
  Kind: string;
  Area: string;
  Subject: string;
  Evidence: string[];
  Count: number;
  Severity: Severity;
  Conf: Conf;
  Tokens?: number;
  USD?: number;
  Minutes?: number;
  Title: string;
  Detail: string;
  Fix?: string;
  FixLang?: string;
  Note?: string;
};

export type Timeline = {
  session_id: string;
  started_at: number;
  rows: TimelineRow[];
  peak_context: number;
  compactions: number;
  receipt: {
    cost_usd: number;
    avoidable_usd: number;
    well_spent: number;
    calls: number;
    tokens: { fresh_input: number; output: number; cache_read: number; cache_write: number };
    by_area_usd: Record<string, number>;
    prompts: { prompt_id: string; label: string; cost_usd: number; tokens: number; flags: number }[];
  };
};

export type Progress = {
  range: string;
  weeks: { week: string; avoidable_usd: number; sessions: number; healthy_pct: number; marked_fixes: number }[];
};

export type Status = { collector_live: boolean; prompts_logged_7d: boolean; last_event_ms?: number };

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: { Accept: "application/json" } });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((body as { error?: string }).error ?? `HTTP ${res.status}`);
  return body as T;
}

async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const out = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((out as { error?: string }).error ?? `HTTP ${res.status}`);
  return out as T;
}

export const api = {
  overview: (range: Range) => get<Overview>(`/api/overview?since=${range}`),
  sessionCards: (range: Range) => get<SessionCards>(`/api/sessions/cards?since=${range}`),
  timeline: (id: string) => get<Timeline>(`/api/sessions/${encodeURIComponent(id)}/timeline`),
  issues: (id: string) =>
    get<{ session_id: string; areas: Record<string, Occurrence[]> }>(`/api/sessions/${encodeURIComponent(id)}/issues`),
  fixes: (range: Range) => get<Fixes>(`/api/fixes?since=${range}`),
  setFixState: (key: string, state: "fixed" | "reverted") =>
    send<{ key: string; state: string }>("POST", `/api/fixes/${encodeURIComponent(key)}/state`, { state }),
  progress: () => get<Progress>(`/api/progress?since=install`),
  status: () => get<Status>(`/api/status`),
  visit: () => send<{ last_visit_at: number; previous_visit_at: number }>("POST", `/api/ui/visit`),
  aiConfig: () => get<{ configured: boolean }>(`/api/ai/config`),
  saveAIKey: (key: string) => send<{ configured: boolean }>("PUT", `/api/ai/config`, { anthropic_api_key: key }),
};
