import { Link } from "react-router-dom";
import type { Insight } from "../lib/api";

const CATEGORY_LABEL: Record<Insight["category"], string> = {
  efficiency: "Efficiency",
  sessions: "Sessions",
  waste: "Waste",
  tools: "Tools & MCP",
};

// InsightBanner renders a single server-computed callout (see
// internal/api/insights.go) as a flag card: a category pill, the title, the
// detail sentence, and a link into the evidence. Renders nothing when no
// insight qualifies — a page with nothing flagged should show no card, not
// an empty box.
export function InsightBanner({ insight }: { insight: Insight | undefined }) {
  if (!insight) return null;

  const isWarn = insight.severity === "warn";

  return (
    <article className={`card flag ${isWarn ? "" : "flag-signal"} flex flex-col`}>
      <div className="flex-1 p-4.5">
        <span className={`pill ${isWarn ? "pill-fault" : "pill-signal"}`}>
          {CATEGORY_LABEL[insight.category]}
        </span>
        <div className="flag-title">{insight.title}</div>
        <p className="flag-text">{insight.detail}</p>
      </div>
      <div className="flex items-center justify-between border-t border-wire px-4.5 py-3">
        <Link to={insight.link} className="text-[13px] text-signal hover:underline">
          View evidence →
        </Link>
      </div>
    </article>
  );
}
