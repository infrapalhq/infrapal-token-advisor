import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { PageShell } from "../components/PageShell";
import { Toolbar, ExportReportButton } from "../components/Toolbar";
import {
  getView,
  type PermissionFriction,
  type McpInventory,
  type ModelPerformance,
} from "../lib/api";
import { formatDurationMs, formatTimestamp } from "../lib/format";

function toolLabel(row: PermissionFriction): string {
  if (row.mcp_server_name) {
    return `${row.mcp_server_name} · ${row.mcp_tool_name ?? "?"}`;
  }
  return row.tool_name;
}

type Filter = "all" | "native" | "mcp" | "failing" | "slow";

export default function Tools() {
  const [usage, setUsage] = useState<PermissionFriction[]>([]);
  const [inventory, setInventory] = useState<McpInventory[]>([]);
  const [modelPerf, setModelPerf] = useState<ModelPerformance[]>([]);
  const [filter, setFilter] = useState<Filter>("all");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getView<PermissionFriction>("permission_friction", { limit: "200" }),
      getView<McpInventory>("mcp_inventory", { limit: "200" }),
      getView<ModelPerformance>("model_performance", { limit: "50" }),
    ])
      .then(([u, i, m]) => {
        setUsage([...u].sort((a, b) => b.total_calls - a.total_calls));
        setInventory(i);
        setModelPerf([...m].sort((a, b) => b.total_requests - a.total_requests));
      })
      .catch((e) => setError(String(e)));
  }, []);

  const deadServers = useMemo(
    () => inventory.filter((s) => s.tool_call_count === 0),
    [inventory],
  );

  const totalErrors = useMemo(
    () => usage.reduce((sum, r) => sum + r.error_count, 0),
    [usage],
  );
  const totalCalls = useMemo(
    () => usage.reduce((sum, r) => sum + r.total_calls, 0),
    [usage],
  );
  const successRate = totalCalls > 0 ? Math.round(((totalCalls - totalErrors) / totalCalls) * 100) : null;
  const slowTools = useMemo(
    () => usage.filter((r) => (r.avg_duration_ms ?? 0) >= 1000),
    [usage],
  );

  const filteredUsage = useMemo(() => {
    switch (filter) {
      case "native":
        return usage.filter((r) => !r.mcp_server_name);
      case "mcp":
        return usage.filter((r) => Boolean(r.mcp_server_name));
      case "failing":
        return usage.filter((r) => r.error_count > 0);
      case "slow":
        return usage.filter((r) => (r.avg_duration_ms ?? 0) >= 1000);
      default:
        return usage;
    }
  }, [usage, filter]);

  const chartData = useMemo(
    () => usage.slice(0, 12).map((r) => ({ label: toolLabel(r), total_calls: r.total_calls })),
    [usage],
  );
  const maxCalls = Math.max(1, ...chartData.map((c) => c.total_calls));
  const callsScaleMax = Math.ceil(maxCalls / 4 / 10) * 10 * 4 || 4;

  return (
    <PageShell
      title="Tools & MCP"
      subtitle="Call volume, success rates and the servers whose definitions ride along on every request."
    >
      <Toolbar right={<ExportReportButton />} />

      {error && <p className="text-sm text-fault">{error}</p>}

      {chartData.length > 0 && (
        <div className="card card-pad">
          <div className="card-title">Tool usage</div>
          <div className="card-desc">Top {chartData.length} tools by call count</div>
          <div className="mt-5 flex flex-col gap-2" style={{ "--label-w": "150px" } as CSSProperties}>
            {chartData.map((c) => (
              <div key={c.label} className="hbar">
                <span className="hbar-label" title={c.label}>{c.label}</span>
                <div className="hbar-val" style={{ width: `${(c.total_calls / callsScaleMax) * 100}%` }} />
              </div>
            ))}
            <div className="hbar">
              <span />
              <div className="hbar-scale">
                <span>0</span>
                <span>{Math.round(callsScaleMax / 4)}</span>
                <span>{Math.round((callsScaleMax / 4) * 2)}</span>
                <span>{Math.round((callsScaleMax / 4) * 3)}</span>
                <span>{callsScaleMax}</span>
              </div>
            </div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <div className="card p-4.5">
          <div className="kpi-label">Total tool calls</div>
          <div className="kpi-value">{totalCalls}</div>
          <div className="kpi-note">across every session in range</div>
        </div>
        <div className="card p-4.5">
          <div className="kpi-label">Success rate</div>
          <div className="kpi-value">{successRate == null ? "—" : `${successRate}%`}</div>
          <div className="kpi-note">{totalErrors} calls returned an error</div>
        </div>
        <div className="card p-4.5">
          <div className="kpi-label">Calls slower than 1s</div>
          <div className="kpi-value">{slowTools.length}</div>
          <div className="kpi-note">tools by average duration</div>
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        {(["all", "native", "mcp", "failing", "slow"] as Filter[]).map((f) => (
          <button
            key={f}
            type="button"
            className={`chip capitalize ${filter === f ? "chip-active" : ""}`}
            onClick={() => setFilter(f)}
          >
            {f === "mcp" ? "MCP" : f}
          </button>
        ))}
      </div>

      <div className="card">
        <div className="card-head">
          <div className="flex items-center gap-3.5">
            <span className="card-icon">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.6 2.6-2-2z" />
              </svg>
            </span>
            <div>
              <div className="card-title">Tools</div>
              <div className="card-desc">Identity, volume, outcome and latency</div>
            </div>
          </div>
        </div>
        <div className="overflow-x-auto">
          <table>
            <thead>
              <tr>
                <th>Tool</th>
                <th className="num">Calls</th>
                <th className="num">Success rate</th>
                <th className="num">Average latency</th>
              </tr>
            </thead>
            <tbody>
              {filteredUsage.map((r) => {
                const total = r.success_count + r.error_count;
                const rate = total > 0 ? Math.round((r.success_count / total) * 100) : null;
                const slow = (r.avg_duration_ms ?? 0) >= 1000;
                return (
                  <tr key={`${r.tool_name}-${r.mcp_server_name}-${r.mcp_tool_name}`}>
                    <td><span className="code">{toolLabel(r)}</span></td>
                    <td className="num">{r.total_calls}</td>
                    <td className={`num ${rate != null && rate < 90 ? "fault" : "good"}`}>
                      {rate == null ? "—" : `${rate}%`}
                    </td>
                    <td className={`num ${slow ? "warn" : ""}`}>{formatDurationMs(r.avg_duration_ms)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {filteredUsage.length === 0 && (
            <div className="empty">No tool calls match this filter.</div>
          )}
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <div className="flex items-center gap-3.5">
            <span className="card-icon">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                <rect x="3" y="4" width="18" height="7" rx="2" />
                <rect x="3" y="13" width="18" height="7" rx="2" />
              </svg>
            </span>
            <div>
              <div className="card-title">MCP inventory</div>
              <div className="card-desc">Connections against actual tool usage</div>
            </div>
          </div>
        </div>
        <div className="overflow-x-auto">
          <table>
            <thead>
              <tr>
                <th>Server</th>
                <th className="num">Connections</th>
                <th>Last connected</th>
                <th className="num">Tool calls</th>
                <th className="num">Distinct tools</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {inventory.map((r) => (
                <tr key={r.server_name}>
                  <td><span className="code">{r.server_name}</span></td>
                  <td className="num">{r.connection_count}</td>
                  <td className="dim">{formatTimestamp(r.last_connected_at)}</td>
                  <td className="num">{r.tool_call_count}</td>
                  <td className="num">{r.distinct_tools_called}</td>
                  <td>
                    <span className={`pill ${r.tool_call_count === 0 ? "pill-warn" : "pill-good"}`}>
                      {r.tool_call_count === 0 ? "Dead config" : "Active"}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {inventory.length === 0 && (
            <div className="empty">No MCP servers seen yet.</div>
          )}
        </div>
        {deadServers.length > 0 && (
          <div className="card-foot">
            {deadServers.length} server{deadServers.length === 1 ? "" : "s"} connected without a single
            tool call. Their tool definitions are sent on every request.
          </div>
        )}
      </div>

      {modelPerf.length > 0 && (
        <div className="card">
          <div className="card-head">
            <div className="flex items-center gap-3.5">
              <span className="card-icon">
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                  <path d="M4 18V8M10 18V4M16 18v-7M22 18h-20" />
                </svg>
              </span>
              <div>
                <div className="card-title">Model performance</div>
                <div className="card-desc">Requests, errors and latency percentiles</div>
              </div>
            </div>
          </div>
          <div className="overflow-x-auto">
            <table>
              <thead>
                <tr>
                  <th>Model</th>
                  <th className="num">Effort</th>
                  <th className="num">Speed</th>
                  <th className="num">Requests</th>
                  <th className="num">Error rate</th>
                  <th className="num">p50 TTFT</th>
                  <th className="num">p95 TTFT</th>
                  <th className="num">p50 duration</th>
                  <th className="num">p95 duration</th>
                </tr>
              </thead>
              <tbody>
                {modelPerf.map((m) => {
                  const errPct = m.error_rate == null ? null : Math.round(m.error_rate * 1000) / 10;
                  return (
                    <tr key={`${m.model}-${m.effort}-${m.speed}`}>
                      <td><span className="code">{m.model}</span></td>
                      <td className="num dim">{m.effort ?? "—"}</td>
                      <td className="num dim">{m.speed ?? "—"}</td>
                      <td className="num">{m.total_requests}</td>
                      <td className={`num ${errPct != null && errPct > 1 ? "warn" : ""}`}>
                        {errPct == null ? "—" : `${errPct}%`}
                      </td>
                      <td className="num">{formatDurationMs(m.p50_ttft_ms)}</td>
                      <td className="num">{formatDurationMs(m.p95_ttft_ms)}</td>
                      <td className="num">{formatDurationMs(m.p50_duration_ms)}</td>
                      <td className="num">{formatDurationMs(m.p95_duration_ms)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </PageShell>
  );
}
