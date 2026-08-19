import { Link } from "react-router-dom";
import type { Insight } from "../lib/api";

// InsightBanner renders a single server-computed callout (see
// internal/api/insights.go) above a page's existing chart/table. Renders
// nothing when no insight qualifies for this page — a page with nothing
// flagged should show no banner, not an empty box.
export function InsightBanner({ insight }: { insight: Insight | undefined }) {
  if (!insight) return null;

  const borderColor = insight.severity === "warn" ? "border-fault" : "border-signal";

  return (
    <div className={`mb-4 flex items-start justify-between gap-4 rounded border border-wire border-l-4 bg-panel p-4 ${borderColor}`}>
      <div>
        <div className="font-display text-sm font-semibold text-ink">{insight.title}</div>
        <p className="mt-1 text-sm text-ink/70">{insight.detail}</p>
      </div>
      <Link
        to={insight.link}
        className="whitespace-nowrap font-mono text-xs text-signal hover:underline"
      >
        Open →
      </Link>
    </div>
  );
}
