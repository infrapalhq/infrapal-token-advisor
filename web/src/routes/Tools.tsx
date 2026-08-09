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
import {
  getView,
  type PermissionFriction,
  type McpInventory,
} from "../lib/api";
import { formatDurationMs, formatTimestamp } from "../lib/format";

function toolLabel(row: PermissionFriction): string {
  if (row.mcp_server_name) {
    return `${row.mcp_server_name} · ${row.mcp_tool_name ?? "?"}`;
  }
  return row.tool_name;
}

export default function Tools() {
  const [usage, setUsage] = useState<PermissionFriction[]>([]);
  const [inventory, setInventory] = useState<McpInventory[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getView<PermissionFriction>("permission_friction", { limit: "200" }),
      getView<McpInventory>("mcp_inventory", { limit: "200" }),
    ])
      .then(([u, i]) => {
        setUsage([...u].sort((a, b) => b.total_calls - a.total_calls));
        setInventory(i);
      })
      .catch((e) => setError(String(e)));
  }, []);

  const deadServers = useMemo(
    () => inventory.filter((s) => s.tool_call_count === 0),
    [inventory],
  );

  const chartData = useMemo(
    () =>
      usage
        .slice(0, 8)
        .map((r) => ({ label: toolLabel(r), total_calls: r.total_calls })),
    [usage],
  );

  return (
    <PageShell
      title="Tools & MCP"
      subtitle="Tool usage, success rate, and latency; MCP servers with dead config"
    >
      {error && <p className="mb-4 text-sm text-fault">{error}</p>}

      <h2 className="mb-2 font-display text-sm font-semibold text-ink/70">
        Tool usage
      </h2>
      {chartData.length > 0 && (
        <div className="mb-4 rounded border border-wire bg-panel p-3">
          <ResponsiveContainer
            width="100%"
            height={Math.max(160, chartData.length * 32)}
          >
            <BarChart
              data={chartData}
              layout="vertical"
              margin={{ top: 4, right: 16, bottom: 4, left: 8 }}
            >
              <CartesianGrid
                horizontal={false}
                stroke="var(--color-wire)"
                strokeDasharray="3 3"
              />
              <XAxis
                type="number"
                allowDecimals={false}
                stroke="var(--color-wire)"
                tick={{ fill: "var(--color-ink)", fontSize: 11 }}
              />
              <YAxis
                type="category"
                dataKey="label"
                width={170}
                stroke="var(--color-wire)"
                tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                tickFormatter={(v: string) =>
                  v.length > 22 ? `${v.slice(0, 21)}…` : v
                }
              />
              <Tooltip
                cursor={{ fill: "var(--color-wire)", opacity: 0.3 }}
                contentStyle={{
                  background: "var(--color-panel)",
                  border: "1px solid var(--color-wire)",
                  borderRadius: 4,
                  fontSize: 11,
                }}
              />
              <Bar
                dataKey="total_calls"
                name="Calls"
                fill="#0284c7"
                maxBarSize={28}
                isAnimationActive={false}
                radius={[0, 3, 3, 0]}
              />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
      <div className="mb-6 overflow-x-auto rounded border border-wire">
        <table className="w-full min-w-[700px] text-left text-sm">
          <thead>
            <tr className="border-b border-wire bg-panel">
              {["Tool", "Calls", "Success rate", "Avg. latency"].map((h) => (
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
            {usage.map((r) => {
              const total = r.success_count + r.error_count;
              const rate = total > 0 ? Math.round((r.success_count / total) * 100) : null;
              return (
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
                  <td
                    className={[
                      "whitespace-nowrap px-3 py-2",
                      rate != null && rate < 90 ? "text-fault" : "text-ink/80",
                    ].join(" ")}
                  >
                    {rate == null ? "—" : `${rate}%`}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                    {formatDurationMs(r.avg_duration_ms)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {usage.length === 0 && (
          <div className="p-4 text-sm text-ink/50">No tool calls recorded yet.</div>
        )}
      </div>

      <h2 className="mb-2 font-display text-sm font-semibold text-ink/70">
        MCP inventory
      </h2>
      <p className="mb-2 text-sm text-ink/50">
        Servers with zero tool calls are dead config — costing tool-definition
        tokens on every request for nothing.
      </p>
      <div className="overflow-x-auto rounded border border-wire">
        <table className="w-full min-w-[700px] text-left text-sm">
          <thead>
            <tr className="border-b border-wire bg-panel">
              {[
                "Server",
                "Connections",
                "Last connected",
                "Tool calls",
                "Distinct tools",
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
            {inventory.map((r) => (
              <tr
                key={r.server_name}
                className={[
                  "border-b border-wire/40 hover:bg-wire/20",
                  r.tool_call_count === 0 ? "bg-fault/5" : "",
                ].join(" ")}
              >
                <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-ink">
                  {r.server_name}
                  {r.tool_call_count === 0 && (
                    <span className="ml-2 text-[10px] uppercase text-fault">
                      dead
                    </span>
                  )}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.connection_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {formatTimestamp(r.last_connected_at)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.tool_call_count}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {r.distinct_tools_called}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {inventory.length === 0 && (
          <div className="p-4 text-sm text-ink/50">No MCP servers seen yet.</div>
        )}
      </div>
      {deadServers.length > 0 && (
        <p className="mt-2 text-xs text-fault">
          {deadServers.length} server{deadServers.length === 1 ? "" : "s"} connected
          but never had a tool called — consider removing from config.
        </p>
      )}
    </PageShell>
  );
}
