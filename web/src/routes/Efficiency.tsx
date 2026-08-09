import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  ScatterChart,
  Scatter,
  XAxis,
  YAxis,
  ZAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { PageShell } from "../components/PageShell";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { type TimeRange } from "../lib/timeRange";
import { getView, type PromptEfficiency } from "../lib/api";
import { formatCostUsd, formatTimestamp } from "../lib/format";

export default function Efficiency() {
  const [range, setRange] = useState<TimeRange>("30d");
  const [rows, setRows] = useState<PromptEfficiency[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getView<PromptEfficiency>("prompt_efficiency", {
      limit: "200",
      ...(range ? { since: range } : {}),
    })
      .then((r) =>
        setRows([...r].sort((a, b) => b.total_cost_usd - a.total_cost_usd)),
      )
      .catch((e) => setError(String(e)));
  }, [range]);

  const scatterData = useMemo(
    () =>
      rows
        .filter((r) => r.prompt_length != null)
        .map((r) => ({
          x: r.prompt_length,
          y: r.llm_turn_count,
          cost: r.total_cost_usd,
          prompt_id: r.prompt_id,
        })),
    [rows],
  );

  return (
    <PageShell
      title="Efficiency"
      subtitle="Prompt-level cost, tokens, and turnaround — sorted by cost, descending"
    >
      <div className="mb-4 flex items-center justify-end">
        <TimeRangeSelect value={range} onChange={setRange} />
      </div>

      {error && <p className="mb-4 text-sm text-fault">{error}</p>}

      <div className="mb-6 rounded border border-wire bg-panel p-3">
        <div className="mb-2 text-[11px] uppercase tracking-wide text-ink/50">
          Prompt length vs. turn count
        </div>
        <div className="h-64">
          <ResponsiveContainer width="100%" height="100%">
            <ScatterChart margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
              <CartesianGrid stroke="var(--color-wire)" strokeDasharray="3 3" />
              <XAxis
                type="number"
                dataKey="x"
                name="prompt length"
                stroke="var(--color-wire)"
                tick={{ fill: "var(--color-ink)", fontSize: 11 }}
              />
              <YAxis
                type="number"
                dataKey="y"
                name="turn count"
                stroke="var(--color-wire)"
                tick={{ fill: "var(--color-ink)", fontSize: 11 }}
              />
              <ZAxis type="number" dataKey="cost" range={[20, 200]} name="cost" />
              <Tooltip
                cursor={{ strokeDasharray: "3 3" }}
                contentStyle={{
                  background: "var(--color-panel)",
                  border: "1px solid var(--color-wire)",
                  borderRadius: 4,
                  fontSize: 12,
                }}
                formatter={(value, name) => [String(value), name]}
              />
              <Scatter data={scatterData} fill="#0284c7" fillOpacity={0.7} />
            </ScatterChart>
          </ResponsiveContainer>
        </div>
      </div>

      <div className="overflow-x-auto rounded border border-wire">
        <table className="w-full min-w-[900px] text-left text-sm">
          <thead>
            <tr className="border-b border-wire bg-panel">
              {[
                "Prompt",
                "Cost",
                "Tokens",
                "Turns",
                "Tool calls",
                "Rejected",
                "Errors",
                "Cache hit",
                "Blocked",
              ].map((h) => (
                <th
                  key={h}
                  className="whitespace-nowrap px-3 py-2 text-[11px] uppercase tracking-wide text-ink/50"
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr
                key={r.prompt_id}
                className="border-b border-wire/40 hover:bg-wire/20"
              >
                <td className="px-3 py-2">
                  <Link
                    to={`/waterfall?session=${r.session_id}&prompt=${r.prompt_id}`}
                    className="font-mono text-xs text-signal hover:underline"
                  >
                    {formatTimestamp(r.started_at)}
                  </Link>
                  {Boolean(r.triggered_compaction || r.hit_max_tokens) && (
                    <span className="ml-2 text-[10px] text-fault">
                      {r.triggered_compaction ? "compacted" : ""}
                      {r.triggered_compaction && r.hit_max_tokens ? " · " : ""}
                      {r.hit_max_tokens ? "max_tokens" : ""}
                    </span>
                  )}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink">
                  {formatCostUsd(r.total_cost_usd)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.total_tokens.toLocaleString()}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.llm_turn_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.tool_call_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-fault">
                  {r.rejected_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-fault">
                  {r.tool_error_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.cache_hit_ratio == null
                    ? "—"
                    : `${Math.round(r.cache_hit_ratio * 100)}%`}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.blocked_on_user_ms > 0 ? `${r.blocked_on_user_ms}ms` : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {rows.length === 0 && (
          <div className="p-4 text-sm text-ink/50">No interactions recorded yet.</div>
        )}
      </div>
    </PageShell>
  );
}
