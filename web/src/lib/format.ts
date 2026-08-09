export function formatDurationMs(ms: number | null | undefined): string {
  if (ms == null) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(s < 10 ? 2 : 1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s % 60);
  return `${m}m ${rem}s`;
}

export function formatCostMicros(micros: number | null | undefined): string {
  if (micros == null) return "—";
  return `$${(micros / 1_000_000).toFixed(4)}`;
}

export function formatCostUsd(usd: number | null | undefined): string {
  if (usd == null) return "—";
  return `$${usd.toFixed(2)}`;
}

export function formatTimestamp(ms: number | null | undefined): string {
  if (ms == null) return "—";
  return new Date(ms).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

// summarizePromptText cleans up prompt_text for display. Most prompts are
// exactly what the user typed and pass through unchanged. Some "prompts" are
// actually the harness re-delivering a background agent's completion as a
// new user turn — a giant <task-notification>...<result>...huge...</result>
// blob, unreadable if shown raw. Extract just the human-relevant summary for
// those; fall back to a generic label rather than ever showing something
// worse than the raw text if the shape isn't what's expected.
export function summarizePromptText(text: string | null | undefined): string {
  if (!text) return "(no prompt text)";
  if (text.startsWith("<task-notification")) {
    const summary = text.match(/<summary>([\s\S]*?)<\/summary>/)?.[1]?.trim();
    return summary
      ? `🔔 Background agent notification: ${summary}`
      : "🔔 Background agent notification";
  }
  return text;
}

export function formatRelative(ms: number | null | undefined): string {
  if (ms == null) return "—";
  const diff = Date.now() - ms;
  const s = Math.round(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.round(h / 24);
  return `${d}d ago`;
}
