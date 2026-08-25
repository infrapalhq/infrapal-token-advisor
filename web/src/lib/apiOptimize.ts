// Dedicated fetch helpers for the Optimize page only. Everything else in
// the app talks to /api (see ../lib/api.ts), which is proxied to whatever
// release build of ccotel is running. This page needs two views and four
// timeseries metrics added on top of that schema (see
// internal/store/migrations/0007_tool_payload_and_retries.sql and the
// tokens_* aliases in internal/api/timeseries.go) — routed through
// /api-dev at a second, locally-built instance so the default /api path
// (and therefore every other route) is untouched.

export type Point = { bucket: string; value: number };

export type SessionLite = {
  session_id: string;
  started_at: number;
  total_tokens: number | null;
  tool_call_count: number | null;
};

export type ToolPayload = {
  tool_name: string;
  mcp_server_name: string | null;
  mcp_tool_name: string | null;
  call_count: number;
  total_input_bytes: number;
  total_result_bytes: number;
  avg_result_bytes: number | null;
  total_result_tokens: number;
};

export type RetryWaste = {
  session_id: string;
  day: string;
  retried_requests: number;
  retried_cost_usd: number;
  retried_tokens: number;
};

async function getDevJSON<T>(path: string): Promise<T> {
  const res = await fetch(`/opt-api${path}`);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `${path}: HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export function getViewDev<T = Record<string, unknown>>(
  name: string,
  params?: Record<string, string>,
): Promise<T[]> {
  const qs = params ? `?${new URLSearchParams(params)}` : "";
  return getDevJSON(`/views/${name}${qs}`);
}

export function getTimeseriesDev(
  metric: string,
  bucket = "day",
  since = "30d",
): Promise<Point[]> {
  return getDevJSON(`/timeseries?metric=${encodeURIComponent(metric)}&bucket=${bucket}&since=${since}`);
}

export function listSessionsDev(limit = 100): Promise<SessionLite[]> {
  return getDevJSON(`/sessions?limit=${limit}`);
}
