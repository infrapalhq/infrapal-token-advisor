import type { CSSProperties } from "react";
import type { Viz } from "../lib/api";
import { formatDurationMs } from "../lib/format";

// MAX_BARS caps how many rows BarViz/StackedViz render — the finding card
// gives this ~60px of height, and each labeled row (a text line plus a
// 12px bar) needs about 17px including its gap, so 3 rows is the most
// that reliably fits without overflowing into the card content below it.
const MAX_BARS = 3;

function formatVizValue(a: number, unit: Viz["unit"]): string {
  switch (unit) {
    case "bytes":
      if (a < 1024) return `${Math.round(a)} B`;
      if (a < 1024 * 1024) return `${(a / 1024).toFixed(1)} KB`;
      return `${(a / (1024 * 1024)).toFixed(2)} MB`;
    case "minutes":
      return formatDurationMs(a * 60000);
    default:
      return String(Math.round(a));
  }
}

// BarViz renders one thin horizontal bar per point, magnitude only — the
// neutral --color-chart token, same as the existing .hbar-val convention
// used elsewhere in the app for a single-series magnitude bar. The value
// is printed at the bar's end rather than left to a hidden axis, per the
// rule that a bar without a number isn't earning its space.
function BarViz({ points, unit }: { points: Viz["points"]; unit: Viz["unit"] }) {
  const rows = points.slice(0, MAX_BARS);
  const max = Math.max(1, ...rows.map((p) => p.a));
  return (
    <div className="flex flex-col gap-1" style={{ "--label-w": "150px" } as CSSProperties}>
      {rows.map((p, idx) => (
        <div key={`${p.label}-${idx}`} className="hbar">
          <span className="hbar-label" title={p.label}>
            {p.label}
          </span>
          <div className="flex items-center gap-2">
            <div className="h-3 flex-1 rounded-sm border border-chart/60 bg-chart/15">
              <div
                className="h-full rounded-sm bg-chart/60"
                style={{ width: `${Math.max(2, (p.a / max) * 100)}%` }}
              />
            </div>
            <span className="shrink-0 whitespace-nowrap font-mono text-[11px] text-ink/60">
              {formatVizValue(p.a, unit)}
            </span>
          </div>
        </div>
      ))}
    </div>
  );
}

// StackedViz renders two bars per point — the full magnitude (A, e.g.
// connections) in the neutral tone, and the actually-used portion (B,
// e.g. calls made) overlaid in the "good" tone. For a dead-config finding
// B is at or near zero, which is itself the shape the card is showing.
function StackedViz({ points, unit }: { points: Viz["points"]; unit: Viz["unit"] }) {
  const rows = points.slice(0, MAX_BARS);
  const max = Math.max(1, ...rows.map((p) => p.a));
  return (
    <div className="flex flex-col gap-1" style={{ "--label-w": "150px" } as CSSProperties}>
      {rows.map((p, idx) => (
        <div key={`${p.label}-${idx}`} className="hbar">
          <span className="hbar-label" title={p.label}>
            {p.label}
          </span>
          <div className="flex items-center gap-2">
            <div className="relative h-3 flex-1 rounded-sm border border-chart/60 bg-chart/15">
              <div
                className="absolute inset-y-0 left-0 rounded-sm bg-chart/50"
                style={{ width: `${Math.max(2, (p.a / max) * 100)}%` }}
              />
              <div
                className="absolute inset-y-0 left-0 rounded-sm bg-good"
                style={{ width: `${Math.min(100, ((p.b ?? 0) / max) * 100)}%` }}
              />
            </div>
            <span className="shrink-0 whitespace-nowrap font-mono text-[11px] text-ink/60">
              {formatVizValue(p.a, unit)} conn · {formatVizValue(p.b ?? 0, unit)} calls
            </span>
          </div>
        </div>
      ))}
    </div>
  );
}

// MiniViz renders a Finding's inline, ~60px-tall breakdown — never a full
// chart. No axes or legend: the finding's evidence sentence already states
// the headline number, this shows its shape and each bar's own value.
export function MiniViz({ viz }: { viz: Viz }) {
  if (!viz.points.length) return null;
  if (viz.type === "stacked") return <StackedViz points={viz.points} unit={viz.unit} />;
  return <BarViz points={viz.points} unit={viz.unit} />;
}
