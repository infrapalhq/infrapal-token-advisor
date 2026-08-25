// Thin fetch wrappers over ccotel's AI Mode endpoints (internal/api/ai.go).
// Mirrors lib/api.ts's getJSON convention. This is ccotel's one deliberate
// network-egress feature: POST /api/ai/recommendations is the only call
// that ever leaves the machine, and only runs when the user clicks
// Generate on the AI Mode page.

export type EvidenceFact = { label: string; value: string };

export type AiRecommendation = {
  id: number;
  batch_id: string;
  generated_at: number;
  category: "efficiency" | "sessions" | "waste" | "tools";
  severity: "info" | "warn";
  title: string;
  detail: string;
  evidence: EvidenceFact[];
};

export type AiRecommendationsResult = {
  batch_id: string;
  generated_at: number;
  from_cache: boolean;
  recommendations: AiRecommendation[];
  dropped_count: number;
};

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `${path}: HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

async function sendJSON<T>(method: "POST" | "PUT", path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const errBody = await res.json().catch(() => ({}));
    throw new Error(errBody.error ?? `${path}: HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export function getAiConfig(): Promise<{ configured: boolean }> {
  return getJSON("/api/ai/config");
}

export function saveAiApiKey(key: string): Promise<{ configured: boolean }> {
  return sendJSON("PUT", "/api/ai/config", { anthropic_api_key: key });
}

export function clearAiApiKey(): Promise<{ configured: boolean }> {
  return sendJSON("PUT", "/api/ai/config", { anthropic_api_key: "" });
}

export function getCachedRecommendations(since?: string): Promise<AiRecommendationsResult> {
  const qs = since ? `?since=${encodeURIComponent(since)}` : "";
  return getJSON(`/api/ai/recommendations${qs}`);
}

export function generateRecommendations(since: string, force = false): Promise<AiRecommendationsResult> {
  return sendJSON("POST", "/api/ai/recommendations", { since, force });
}
