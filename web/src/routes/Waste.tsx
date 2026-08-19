import { useEffect, useMemo, useState } from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { PageShell } from "../components/PageShell";
import { InsightBanner } from "../components/InsightBanner";
import {
  getView,
  getInsights,
  type Insight,
  type WastedSpend,
  type PermissionFriction,
} from "../lib/api";
import { formatCostUsd, formatDurationMs } from "../lib/format";

function toolLabel(row: PermissionFriction): string {
  if (row.mcp_server_name) {
    return `${row.mcp_server_name} · ${row.mcp_tool_name ?? "?"}`;
  }
  return row.tool_name;
}

export default function Waste() {
  const [wasted, setWasted] = useState<WastedSpend[]>([]);
  const [friction, setFriction] = useState<PermissionFriction[]>([]);
  const [insights, setInsights] = useState<Insight[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getView<WastedSpend>("wasted_spend", { limit: "200" }),
      getView<PermissionFriction>("permission_friction", { limit: "200" }),
    ])
      .then(([w, f]) => {
        setWasted([...w].sort((a, b) => b.wasted_cost_usd - a.wasted_cost_usd));
        setFriction(
          [...f]
            .filter((r) => r.user_temporary_count > 0)
            .sort((a, b) => b.user_temporary_count - a.user_temporary_count),
        );
      })
      .catch((e) => setError(String(e)));
  }, []);

  useEffect(() => {
    getInsights()
      .then((all) => setInsights(all.filter((i) => i.category === "waste")))
      .catch(() => setInsights([]));
  }, []);

  const totalWasted = useMemo(
    () => wasted.reduce((sum, r) => sum + r.wasted_cost_usd, 0),
    [wasted],
  );
  const totalRejected = useMemo(
    () => wasted.reduce((sum, r) => sum + r.rejected_tool_count, 0),
    [wasted],
  );

  const dailyWaste = useMemo(() => {
    const byDay = new Map<string, number>();
    for (const r of wasted) {
      byDay.set(r.day, (byDay.get(r.day) ?? 0) + r.wasted_cost_usd);
    }
    return [...byDay.entries()]
      .map(([day, wasted_cost_usd]) => ({ day, wasted_cost_usd }))
      .sort((a, b) => a.day.localeCompare(b.day));
  }, [wasted]);

  return (
    <PageShell
      title="Waste"
      subtitle="Rejected-work cost and permission friction — where to add allow-rules"
    >
      {error && <p className="mb-4 text-sm text-fault">{error}</p>}

      {insights.map((i) => (
        <InsightBanner key={i.id} insight={i} />
      ))}

      <div className="mb-6 grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div className="rounded border border-wire bg-panel p-4">
          <div className="text-[11px] uppercase tracking-wide text-ink/50">
            Wasted spend (approx.)
          </div>
          <div className="mt-1 font-mono text-2xl text-fault">
            {formatCostUsd(totalWasted)}
          </div>
        </div>
        <div className="rounded border border-wire bg-panel p-4">
          <div className="text-[11px] uppercase tracking-wide text-ink/50">
            Rejected tool calls
          </div>
          <div className="mt-1 font-mono text-2xl text-ink">{totalRejected}</div>
        </div>
      </div>

      <h2 className="mb-2 font-display text-sm font-semibold text-ink/70">
        Wasted spend by session / day
      </h2>
      {dailyWaste.length > 0 && (
        <div className="mb-4 rounded border border-wire bg-panel p-3">
          <ResponsiveContainer width="100%" height={200}>
            <BarChart
              data={dailyWaste}
              margin={{ top: 8, right: 12, bottom: 4, left: 0 }}
            >
              <CartesianGrid
                vertical={false}
                stroke="var(--color-wire)"
                strokeDasharray="3 3"
              />
              <XAxis
                dataKey="day"
                stroke="var(--color-wire)"
                tick={{ fill: "var(--color-ink)", fontSize: 11 }}
              />
              <YAxis
                domain={[0, "auto"]}
                stroke="var(--color-wire)"
                tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                tickFormatter={(v: number) => formatCostUsd(v)}
                width={64}
              />
              <Tooltip
                cursor={{ fill: "var(--color-wire)", opacity: 0.3 }}
                contentStyle={{
                  background: "var(--color-panel)",
                  border: "1px solid var(--color-wire)",
                  borderRadius: 4,
                  fontSize: 11,
                }}
                formatter={(v) => [formatCostUsd(Number(v ?? 0)), "Wasted"]}
              />
              <Bar
                dataKey="wasted_cost_usd"
                name="Wasted"
                fill="#dc2626"
                maxBarSize={64}
                isAnimationActive={false}
                radius={[3, 3, 0, 0]}
              />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
      <div className="mb-6 overflow-x-auto rounded border border-wire">
        <table className="w-full min-w-[600px] text-left text-sm">
          <thead>
            <tr className="border-b border-wire bg-panel">
              {["Session", "Day", "Wasted cost", "Rejected calls"].map((h) => (
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
            {wasted.map((r) => (
              <tr
                key={`${r.session_id}-${r.day}`}
                className="border-b border-wire/40 hover:bg-wire/20"
              >
                <td className="px-3 py-2 font-mono text-xs text-ink/80">
                  {r.session_id.slice(0, 8)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">{r.day}</td>
                <td className="whitespace-nowrap px-3 py-2 text-fault">
                  {formatCostUsd(r.wasted_cost_usd)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.rejected_tool_count}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {wasted.length === 0 && (
          <div className="p-4 text-sm text-ink/50">
            No rejected tool calls recorded — nothing wasted yet.
          </div>
        )}
      </div>

      <h2 className="mb-2 font-display text-sm font-semibold text-ink/70">
        Allow-rules you should add
      </h2>
      <p className="mb-2 text-sm text-ink/50">
        Tools you keep temporarily allowing instead of permanently — each
        prompt costs you the latency in the last column.
      </p>
      <div className="overflow-x-auto rounded border border-wire">
        <table className="w-full min-w-[700px] text-left text-sm">
          <thead>
            <tr className="border-b border-wire bg-panel">
              {[
                "Tool",
                "Total calls",
                "Temporary allows",
                "Permanent allows",
                "Time spent waiting",
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
            {friction.map((r) => (
              <tr
                key={`${r.tool_name}-${r.mcp_server_name}-${r.mcp_tool_name}`}
                className="border-b border-wire/40 hover:bg-wire/20"
              >
                <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-signal">
                  {toolLabel(r)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.total_calls}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink">
                  {r.user_temporary_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/50">
                  {r.user_permanent_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-fault">
                  {formatDurationMs(r.total_blocked_on_user_ms)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {friction.length === 0 && (
          <div className="p-4 text-sm text-ink/50">
            No repeated permission prompts — nothing to promote to an allow-rule.
          </div>
        )}
      </div>
    </PageShell>
  );
}
