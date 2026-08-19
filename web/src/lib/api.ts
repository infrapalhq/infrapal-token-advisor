// Thin fetch wrappers over ccotel's read-only JSON API
// (internal/api/server.go). Every numeric SQLite column comes back as a
// JSON number; nullable columns come back as `null`; SQLite's 0/1 booleans
// (e.g. tool_calls.success) come back as plain numbers, not JSON booleans.

export type Session = {
  session_id: string;
  started_at: number;
  ended_at: number | null;
  start_type: string | null;
  app_version: string | null;
  terminal_type: string | null;
  duration_ms: number | null;
  total_cost_usd: number | null;
  total_tokens: number | null;
  lines_added: number | null;
  lines_removed: number | null;
  commit_count: number | null;
  pull_request_count: number | null;
  active_time_user_s: number | null;
  active_time_cli_s: number | null;
  tool_call_count: number | null;
  compaction_count: number | null;
};

export type SessionInteraction = {
  prompt_id: string;
  seq: number;
  started_at: number;
  duration_ms: number | null;
  prompt_text: string | null;
  command_name: string | null;
};

export type InteractionSummary = {
  prompt_id: string;
  session_id: string;
  seq: number;
  started_at: number;
  duration_ms: number | null;
  prompt_text: string | null;
  prompt_length: number | null;
  command_name: string | null;
  total_cost_usd: number;
  total_tokens: number;
  llm_turn_count: number;
  tool_call_count: number;
};

export function listInteractions(
  limit = 50,
  offset = 0,
  since?: string,
): Promise<InteractionSummary[]> {
  const qs = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (since) qs.set("since", since);
  return getJSON(`/api/interactions?${qs}`);
}

export type SessionDetail = {
  session: Session & {
    app_entrypoint: string | null;
    user_id: string | null;
    user_email: string | null;
    org_id: string | null;
  };
  interactions: SessionInteraction[];
};

export type Interaction = {
  prompt_id: string;
  session_id: string;
  seq: number;
  started_at: number;
  duration_ms: number | null;
  prompt_text: string | null;
  prompt_length: number | null;
  command_name: string | null;
  command_source: string | null;
  message_uuid: string | null;
  trace_id: string | null;
  root_span_id: string | null;
};

export type Span = {
  span_id: string;
  trace_id: string | null;
  parent_span_id: string | null;
  span_type: string | null;
  name: string | null;
  start_ns: number;
  end_ns: number | null;
  status: string | null;
  session_id: string | null;
  agent_id: string | null;
  parent_agent_id: string | null;
  tool_use_id: string | null;
};

export type ApiRequest = {
  request_id: string;
  ts: number;
  model: string | null;
  query_source: string | null;
  speed: string | null;
  effort: string | null;
  input_tokens: number | null;
  output_tokens: number | null;
  cache_read_tokens: number | null;
  cache_creation_tokens: number | null;
  cost_usd_micros: number | null;
  duration_ms: number | null;
  ttft_ms: number | null;
  stop_reason: string | null;
  attempt: number | null;
  success: number | null;
  status_code: number | null;
  error: string | null;
};

export type ToolCall = {
  tool_use_id: string;
  ts: number;
  tool_name: string | null;
  tool_source: string | null;
  decision: string | null;
  decision_source: string | null;
  blocked_on_user_ms: number | null;
  duration_ms: number | null;
  success: number | null;
  error_type: string | null;
  error: string | null;
  mcp_server_name: string | null;
  mcp_tool_name: string | null;
  skill_name: string | null;
  subagent_type: string | null;
  full_command: string | null;
};

export type AssistantResponse = {
  message_uuid: string;
  ts: number;
  model: string | null;
  request_id: string | null;
  query_source: string | null;
  response_text: string | null;
  response_length: number | null;
};

export type InteractionDetail = {
  interaction: Interaction;
  spans: Span[];
  api_requests: ApiRequest[];
  tool_calls: ToolCall[];
  assistant_responses: AssistantResponse[];
};

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `${path}: HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export function listSessions(
  limit = 50,
  offset = 0,
  since?: string,
): Promise<Session[]> {
  const qs = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (since) qs.set("since", since);
  return getJSON(`/api/sessions?${qs}`);
}

export function getSession(id: string): Promise<SessionDetail> {
  return getJSON(`/api/sessions/${encodeURIComponent(id)}`);
}

export function getInteraction(promptId: string): Promise<InteractionDetail> {
  return getJSON(`/api/interactions/${encodeURIComponent(promptId)}`);
}

export function searchEvents(q: string): Promise<SearchHit[]> {
  return getJSON(`/api/search?q=${encodeURIComponent(q)}`);
}

export type PromptEfficiency = {
  prompt_id: string;
  session_id: string;
  started_at: number;
  duration_ms: number | null;
  prompt_length: number | null;
  llm_turn_count: number;
  total_tokens: number;
  total_cost_usd: number;
  cache_hit_ratio: number | null;
  tool_call_count: number;
  rejected_count: number;
  tool_error_count: number;
  blocked_on_user_ms: number;
  triggered_compaction: number;
  hit_max_tokens: number;
};

export type WastedSpend = {
  session_id: string;
  day: string;
  wasted_cost_usd: number;
  rejected_tool_count: number;
};

export type PermissionFriction = {
  tool_name: string;
  mcp_server_name: string | null;
  mcp_tool_name: string | null;
  total_calls: number;
  user_temporary_count: number;
  user_permanent_count: number;
  total_blocked_on_user_ms: number;
  success_count: number;
  error_count: number;
  avg_duration_ms: number | null;
};

export type McpInventory = {
  server_name: string;
  connection_count: number;
  last_connected_at: number | null;
  tool_call_count: number;
  distinct_tools_called: number;
};

export type ModelPerformance = {
  model: string;
  effort: string | null;
  speed: string | null;
  total_requests: number;
  error_count: number;
  error_rate: number | null;
  p50_ttft_ms: number | null;
  p95_ttft_ms: number | null;
  p50_duration_ms: number | null;
  p95_duration_ms: number | null;
};

export type SearchHit = {
  kind: "prompt" | "response";
  id: string;
  session_id: string;
  ts: number;
  snippet: string;
};

export function getView<T = Record<string, unknown>>(
  name: string,
  params?: Record<string, string>,
): Promise<T[]> {
  const qs = params ? `?${new URLSearchParams(params)}` : "";
  return getJSON(`/api/views/${name}${qs}`);
}

export function getTimeseries(
  metric: string,
  bucket = "day",
  since = "7d",
): Promise<{ bucket: string; value: number }[]> {
  return getJSON(
    `/api/timeseries?metric=${encodeURIComponent(metric)}&bucket=${bucket}&since=${since}`,
  );
}

export type Insight = {
  id: string;
  category: "efficiency" | "sessions" | "waste" | "tools";
  severity: "info" | "warn";
  title: string;
  detail: string;
  link: string;
};

export function getInsights(since?: string): Promise<Insight[]> {
  const qs = since ? `?since=${encodeURIComponent(since)}` : "";
  return getJSON(`/api/insights${qs}`);
}
