import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  BarChart,
  Bar,
  ComposedChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { PageShell } from "../components/PageShell";
import { Toolbar, ExportReportButton } from "../components/Toolbar";
import { Verdict } from "../components/Verdict";
import { useDateRange } from "../lib/dateRange";
import {
  listSessions,
  getSession,
  getView,
  getTimeseries,
  type Session,
  type PermissionFriction,
  type McpInventory,
  type ModelPerformance,
  type ToolPayload,
  type AncillaryModelSpend,
  type Point,
} from "../lib/api";
import { formatCostUsd, formatDurationMs, formatTimestamp, formatTokenCount, summarizePromptText } from "../lib/format";

const tooltipStyle = {
  background: "var(--color-panel-2)",
  border: "1px solid var(--color-wire-2)",
  borderRadius: 6,
  fontSize: 12,
};

// ---- Token composition (stacked bars) ----------------------------------

type Composed = { bucket: string; input: number; output: number; cacheRead: number; cacheCreation: number };

function mergeTokenSeries(input: Point[], output: Point[], cacheRead: Point[], cacheCreation: Point[]): Composed[] {
  const buckets = new Map<string, Composed>();
  const put = (points: Point[], key: keyof Omit<Composed, "bucket">) => {
    for (const p of points) {
      const row = buckets.get(p.bucket) ?? { bucket: p.bucket, input: 0, output: 0, cacheRead: 0, cacheCreation: 0 };
      row[key] = p.value;
      buckets.set(p.bucket, row);
    }
  };
  put(input, "input");
  put(output, "output");
  put(cacheRead, "cacheRead");
  put(cacheCreation, "cacheCreation");
  return [...buckets.values()].sort((a, b) => a.bucket.localeCompare(b.bucket));
}

// AMBER_ALERT_PCT: below this share of cache-creation-among-cacheable-
// tokens, the composition chart has nothing to say — "everything's fine"
// isn't worth its full size, so it collapses to a one-line strip instead.
const AMBER_ALERT_PCT = 15;

// ---- Cost concentration (Pareto) ---------------------------------------

const CONCENTRATION_TOP_N = 12;

type ConcentrationRow = {
  session_id: string;
  label: string;
  cost: number;
  pctOfRange: number;
  cumulativePct: number;
};

// firstPromptLabel resolves a session to a short, human label — its first
// prompt's text (or command name), not its hash, which means nothing at a
// glance. Falls back to the hash only when no interaction has any text.
async function firstPromptLabel(sessionId: string): Promise<string> {
  try {
    const detail = await getSession(sessionId);
    const first = detail.interactions[0];
    if (!first) return sessionId.slice(0, 8);
    const text = first.command_name ?? summarizePromptText(first.prompt_text);
    if (!text || text === "(no prompt text)") return sessionId.slice(0, 8);
    return text.length > 42 ? `${text.slice(0, 42)}…` : text;
  } catch {
    return sessionId.slice(0, 8);
  }
}

async function fetchConcentration(top: Session[]): Promise<ConcentrationRow[]> {
  const totalCost = top.reduce((sum, s) => sum + (s.total_cost_usd ?? 0), 0);
  const labels = await Promise.all(top.map((s) => firstPromptLabel(s.session_id)));
  let cumulative = 0;
  return top.map((s, i) => {
    const cost = s.total_cost_usd ?? 0;
    const pctOfRange = totalCost > 0 ? (cost / totalCost) * 100 : 0;
    cumulative += pctOfRange;
    return { session_id: s.session_id, label: labels[i], cost, pctOfRange, cumulativePct: cumulative };
  });
}

// ---- Tool identity + row merging ----------------------------------------
//
// v_tool_payload/v_permission_friction group by (tool_name, mcp_server_name,
// mcp_tool_name) — the finest identity the schema has. But Claude Code
// records some MCP calls two different ways (a direct
// "mcp__server__tool" tool_name, and the same logical call proxied through
// a generic "mcp_tool" dispatcher tool_name), which collapse to the same
// *display* label. Left unmerged, that reads as a duplicate-row bug in
// every table keyed by label — so every table here merges by display label
// before rendering, and sums/re-weights the underlying rows.

function toolLabel(toolName: string, mcpServer: string | null, mcpTool: string | null): string {
  if (mcpServer) return `${mcpServer} · ${mcpTool ?? "?"}`;
  return toolName;
}

type MergedFriction = {
  label: string;
  totalCalls: number;
  successCount: number;
  errorCount: number;
  totalBlockedMs: number;
  avgDurationMs: number | null;
  isMcp: boolean;
};

function mergeFriction(rows: PermissionFriction[]): MergedFriction[] {
  const byLabel = new Map<string, MergedFriction & { durationWeight: number }>();
  for (const r of rows) {
    const label = toolLabel(r.tool_name, r.mcp_server_name, r.mcp_tool_name);
    const existing = byLabel.get(label);
    const weight = r.total_calls * (r.avg_duration_ms ?? 0);
    if (existing) {
      existing.totalCalls += r.total_calls;
      existing.successCount += r.success_count;
      existing.errorCount += r.error_count;
      existing.totalBlockedMs += r.total_blocked_on_user_ms;
      existing.durationWeight += weight;
    } else {
      byLabel.set(label, {
        label,
        totalCalls: r.total_calls,
        successCount: r.success_count,
        errorCount: r.error_count,
        totalBlockedMs: r.total_blocked_on_user_ms,
        avgDurationMs: null,
        isMcp: Boolean(r.mcp_server_name),
        durationWeight: weight,
      });
    }
  }
  return [...byLabel.values()]
    .map((r) => ({ ...r, avgDurationMs: r.totalCalls > 0 ? r.durationWeight / r.totalCalls : null }))
    .sort((a, b) => b.totalCalls - a.totalCalls);
}

type MergedPayload = {
  label: string;
  callCount: number;
  totalResultBytes: number;
};

function mergePayload(rows: ToolPayload[]): MergedPayload[] {
  const byLabel = new Map<string, MergedPayload>();
  for (const r of rows) {
    const label = toolLabel(r.tool_name, r.mcp_server_name, r.mcp_tool_name);
    const existing = byLabel.get(label);
    if (existing) {
      existing.callCount += r.call_count;
      existing.totalResultBytes += r.total_result_bytes;
    } else {
      byLabel.set(label, { label, callCount: r.call_count, totalResultBytes: r.total_result_bytes });
    }
  }
  return [...byLabel.values()].sort((a, b) => b.totalResultBytes - a.totalResultBytes);
}

function formatBytes(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return "—";
  if (n < 1024) return `${Math.round(n)} B`;
  const kb = n / 1024;
  if (kb < 1024) return `${kb.toFixed(1)} KB`;
  return `${(kb / 1024).toFixed(2)} MB`;
}

// ---- Tool cost matrix (horizontal bar, sorted by bytes) ------------------

const TOOL_MATRIX_TOP_N = 10;

type ToolMatrixRow = {
  label: string;
  healthyBytes: number;
  failedBytes: number;
  failureRatePct: number;
  totalBytes: number;
};

function buildToolMatrix(payload: MergedPayload[], friction: MergedFriction[]): ToolMatrixRow[] {
  const frictionByLabel = new Map(friction.map((r) => [r.label, r]));
  return payload
    .filter((p) => p.callCount > 0)
    .map((p) => {
      const f = frictionByLabel.get(p.label);
      const total = f ? f.successCount + f.errorCount : 0;
      const failureRatePct = total > 0 ? (f!.errorCount / total) * 100 : 0;
      return {
        label: p.label,
        healthyBytes: p.totalResultBytes * (1 - failureRatePct / 100),
        failedBytes: p.totalResultBytes * (failureRatePct / 100),
        failureRatePct,
        totalBytes: p.totalResultBytes,
      };
    })
    .sort((a, b) => b.totalBytes - a.totalBytes)
    .slice(0, TOOL_MATRIX_TOP_N);
}

// ---- Sessions table -----------------------------------------------------

const TOOLS_TABLE_PAGE_SIZE = 15;

type SortKey =
  | "started_at"
  | "duration_ms"
  | "active_time_ms"
  | "total_cost_usd"
  | "total_tokens"
  | "tool_call_count"
  | "lines_added";

const COLUMNS: { key: SortKey; label: string }[] = [
  { key: "started_at", label: "Started" },
  { key: "duration_ms", label: "Wall clock" },
  { key: "active_time_ms", label: "Active work" },
  { key: "total_cost_usd", label: "Cost" },
  { key: "total_tokens", label: "Tokens" },
  { key: "tool_call_count", label: "Tool calls" },
  { key: "lines_added", label: "+Lines" },
];

// ---- Tools & Models tab --------------------------------------------------

type ToolsFilter = "all" | "native" | "mcp" | "failing" | "slow";

export default function Spend() {
  const navigate = useNavigate();
  const { range } = useDateRange();

  const [sessions, setSessions] = useState<Session[]>([]);
  const [tokenSeries, setTokenSeries] = useState<Composed[]>([]);
  const [concentration, setConcentration] = useState<ConcentrationRow[]>([]);
  const [sessionLabels, setSessionLabels] = useState<Map<string, string>>(new Map());
  const [toolPayload, setToolPayload] = useState<ToolPayload[]>([]);
  const [friction, setFriction] = useState<PermissionFriction[]>([]);
  const [mcpInventory, setMcpInventory] = useState<McpInventory[]>([]);
  const [modelPerf, setModelPerf] = useState<ModelPerformance[]>([]);
  const [ancillarySpend, setAncillarySpend] = useState<AncillaryModelSpend[]>([]);
  const [error, setError] = useState<string | null>(null);

  const [sortKey, setSortKey] = useState<SortKey>("total_cost_usd");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");
  const [toolsTab, setToolsTab] = useState<"tools" | "models">("tools");
  const [toolsFilter, setToolsFilter] = useState<ToolsFilter>("all");
  const [toolsShowAll, setToolsShowAll] = useState(false);

  useEffect(() => {
    listSessions(200, 0, range || undefined)
      .then(setSessions)
      .catch((e) => setError(String(e)));
  }, [range]);

  useEffect(() => {
    const since = range || "90d";
    Promise.all([
      getTimeseries("tokens_input", "day", since),
      getTimeseries("tokens_output", "day", since),
      getTimeseries("tokens_cache_read", "day", since),
      getTimeseries("tokens_cache_creation", "day", since),
    ])
      .then(([i, o, cr, cc]) => setTokenSeries(mergeTokenSeries(i, o, cr, cc)))
      .catch((e) => setError(String(e)));
  }, [range]);

  useEffect(() => {
    Promise.all([
      getView<ToolPayload>("tool_payload", { limit: "500" }),
      getView<PermissionFriction>("permission_friction", { limit: "200" }),
      getView<McpInventory>("mcp_inventory", { limit: "200" }),
      getView<ModelPerformance>("model_performance", { limit: "50" }),
      getView<AncillaryModelSpend>("ancillary_model_spend", { limit: "50" }),
    ])
      .then(([payload, fric, inv, models, ancillary]) => {
        setToolPayload(payload);
        setFriction(fric);
        setMcpInventory(inv);
        setModelPerf([...models].sort((a, b) => b.total_requests - a.total_requests));
        setAncillarySpend(ancillary);
      })
      .catch((e) => setError(String(e)));
  }, []);

  useEffect(() => {
    if (sessions.length === 0) {
      setConcentration([]);
      return;
    }
    const top = [...sessions].sort((a, b) => (b.total_cost_usd ?? 0) - (a.total_cost_usd ?? 0)).slice(0, CONCENTRATION_TOP_N);
    fetchConcentration(top)
      .then((rows) => {
        setConcentration(rows);
        setSessionLabels(new Map(rows.map((r) => [r.session_id, r.label])));
      })
      .catch((e) => setError(String(e)));
  }, [sessions]);

  // ---- header strip ----

  const totalTokens = useMemo(
    () => tokenSeries.reduce((s, d) => s + d.input + d.output + d.cacheRead + d.cacheCreation, 0),
    [tokenSeries],
  );
  const totalCost = useMemo(() => sessions.reduce((s, r) => s + (r.total_cost_usd ?? 0), 0), [sessions]);
  const cacheRead = useMemo(() => tokenSeries.reduce((s, d) => s + d.cacheRead, 0), [tokenSeries]);
  const cacheCreation = useMemo(() => tokenSeries.reduce((s, d) => s + d.cacheCreation, 0), [tokenSeries]);
  const cacheHitRatio = cacheRead + cacheCreation > 0 ? (cacheRead / (cacheRead + cacheCreation)) * 100 : null;
  const amberPct = cacheRead + cacheCreation > 0 ? (cacheCreation / (cacheRead + cacheCreation)) * 100 : 0;
  const wallClockMs = useMemo(() => sessions.reduce((s, r) => s + (r.duration_ms ?? 0), 0), [sessions]);
  const activeMs = useMemo(
    () => sessions.reduce((s, r) => s + ((r.active_time_user_s ?? 0) + (r.active_time_cli_s ?? 0)) * 1000, 0),
    [sessions],
  );
  const activeSharePct = wallClockMs > 0 ? (activeMs / wallClockMs) * 100 : null;

  const ancillaryTotalUSD = useMemo(() => ancillarySpend.reduce((s, r) => s + r.cost_usd, 0), [ancillarySpend]);
  const ancillaryModel = ancillarySpend[0]?.model;

  // ---- sessions table ----

  const sorted = useMemo(() => {
    const rows = sessions.map((s) => ({
      ...s,
      active_time_ms: ((s.active_time_user_s ?? 0) + (s.active_time_cli_s ?? 0)) * 1000,
    }));
    rows.sort((a, b) => {
      const av = a[sortKey] ?? 0;
      const bv = b[sortKey] ?? 0;
      return sortDir === "asc" ? av - bv : bv - av;
    });
    return rows;
  }, [sessions, sortKey, sortDir]);

  function toggleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  }

  // ---- merged tool identity ----

  const mergedFriction = useMemo(() => mergeFriction(friction), [friction]);
  const mergedPayload = useMemo(() => mergePayload(toolPayload), [toolPayload]);
  const toolMatrix = useMemo(() => buildToolMatrix(mergedPayload, mergedFriction), [mergedPayload, mergedFriction]);

  // ---- tools tab ----

  const filteredUsage = useMemo(() => {
    switch (toolsFilter) {
      case "native":
        return mergedFriction.filter((r) => !r.isMcp);
      case "mcp":
        return mergedFriction.filter((r) => r.isMcp);
      case "failing":
        return mergedFriction.filter((r) => r.errorCount > 0);
      case "slow":
        return mergedFriction.filter((r) => (r.avgDurationMs ?? 0) >= 1000);
      default:
        return mergedFriction;
    }
  }, [mergedFriction, toolsFilter]);

  const visibleUsage = toolsShowAll ? filteredUsage : filteredUsage.slice(0, TOOLS_TABLE_PAGE_SIZE);

  const deadServerCount = useMemo(() => mcpInventory.filter((s) => s.tool_call_count === 0).length, [mcpInventory]);

  return (
    <PageShell title="Spend" subtitle="Where the tokens and dollars actually went.">
      <Toolbar right={<ExportReportButton />} />

      {error && <p className="text-sm text-fault">{error}</p>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Verdict label="Tokens" value={formatTokenCount(totalTokens)} note="this range" />
        <Verdict label="Cost" value={formatCostUsd(totalCost)} note="this range" />
        <Verdict
          label="Cache hit"
          value={cacheHitRatio == null ? "—" : `${Math.round(cacheHitRatio)}%`}
          tone={cacheHitRatio == null ? "neutral" : cacheHitRatio >= 80 ? "good" : cacheHitRatio >= 50 ? "warn" : "bad"}
          note={cacheHitRatio == null ? "no cacheable tokens yet" : cacheHitRatio >= 80 ? "healthy reuse" : "context is churning"}
        />
        <Verdict
          label="Active vs. wall clock"
          value={activeSharePct == null ? "—" : `${Math.round(activeSharePct)}%`}
          tone={activeSharePct == null ? "neutral" : activeSharePct >= 20 ? "good" : "warn"}
          note={
            activeSharePct == null
              ? "no sessions yet"
              : `${formatDurationMs(activeMs)} active of ${formatDurationMs(wallClockMs)} wall clock`
          }
        />
      </div>

      {amberPct < AMBER_ALERT_PCT ? (
        <div className="card flex items-center justify-between gap-4 p-4.5">
          <div>
            <div className="card-title">Token composition</div>
            <div className="card-desc">
              Cache creation is {amberPct.toFixed(1)}% of cacheable tokens — nothing to flag here.
            </div>
          </div>
          <div className="h-2 w-40 overflow-hidden rounded-full bg-good/20">
            <div className="h-full rounded-full bg-good" style={{ width: `${Math.max(4, 100 - amberPct)}%` }} />
          </div>
        </div>
      ) : (
        <div className="card">
          <div className="card-head">
            <div>
              <div className="card-title">Token composition</div>
              <div className="card-desc">Input / output / cache-read / cache-creation tokens per day</div>
            </div>
          </div>
          <div className="card-pad">
            <div className="h-64">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={tokenSeries} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
                  <CartesianGrid vertical={false} stroke="var(--color-wire)" strokeDasharray="3 4" />
                  <XAxis dataKey="bucket" stroke="var(--color-wire)" tick={{ fill: "var(--color-ink)", fontSize: 11 }} />
                  <YAxis
                    stroke="var(--color-wire)"
                    tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                    tickFormatter={(v) => formatTokenCount(Number(v))}
                    width={48}
                  />
                  <Tooltip
                    contentStyle={tooltipStyle}
                    labelStyle={{ color: "var(--color-ink)" }}
                    formatter={(v, name) => [formatTokenCount(Number(v ?? 0)), name]}
                  />
                  <Bar dataKey="input" name="input" stackId="tok" fill="var(--color-cat-llm)" radius={0} stroke="var(--color-panel)" strokeWidth={1} />
                  <Bar dataKey="output" name="output" stackId="tok" fill="var(--color-signal)" radius={0} stroke="var(--color-panel)" strokeWidth={1} />
                  <Bar dataKey="cacheRead" name="cache read" stackId="tok" fill="var(--color-good)" radius={0} stroke="var(--color-panel)" strokeWidth={1} />
                  <Bar dataKey="cacheCreation" name="cache creation" stackId="tok" fill="var(--color-warn)" radius={[3, 3, 0, 0]} stroke="var(--color-panel)" strokeWidth={1} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
          <div className="card-foot">
            Green (cache read) is cheap reuse; amber (cache creation) is a full rebuild. Amber crowding out green is
            cache thrashing, visible instantly.
          </div>
        </div>
      )}

      {concentration.length > 0 && (
        <div className="card">
          <div className="card-head">
            <div>
              <div className="card-title">Cost concentration</div>
              <div className="card-desc">
                Share of total spend per session (bars) and the running total (line) — both on the same 0–100% axis
              </div>
            </div>
          </div>
          <div className="card-pad">
            <div className="h-72">
              <ResponsiveContainer width="100%" height="100%">
                <ComposedChart data={concentration} margin={{ top: 8, right: 16, bottom: 64, left: 0 }}>
                  <CartesianGrid vertical={false} stroke="var(--color-wire)" strokeDasharray="3 4" />
                  <XAxis
                    dataKey="label"
                    stroke="var(--color-wire)"
                    tick={{ fill: "var(--color-ink)", fontSize: 10 }}
                    angle={-35}
                    textAnchor="end"
                    interval={0}
                    height={70}
                  />
                  <YAxis
                    domain={[0, 100]}
                    stroke="var(--color-wire)"
                    tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                    tickFormatter={(v) => `${v}%`}
                    width={40}
                  />
                  <Tooltip
                    contentStyle={tooltipStyle}
                    labelStyle={{ color: "var(--color-ink)" }}
                    formatter={(v, name) => [`${Number(v).toFixed(1)}%`, name]}
                  />
                  <Bar dataKey="pctOfRange" name="% of spend" fill="var(--color-chart)" radius={[3, 3, 0, 0]} />
                  <Line type="monotone" dataKey="cumulativePct" name="cumulative %" stroke="var(--color-fault)" strokeWidth={2} dot={false} isAnimationActive={false} />
                </ComposedChart>
              </ResponsiveContainer>
            </div>
          </div>
          <div className="card-foot">
            {(() => {
              const crossing = concentration.findIndex((r) => r.cumulativePct >= 80);
              if (crossing < 0) return "No small set of sessions dominates this range's spend.";
              return `${crossing + 1} of ${concentration.length} sessions shown account for ${concentration[crossing].cumulativePct.toFixed(0)}% of spend.`;
            })()}
          </div>
        </div>
      )}

      {toolMatrix.length > 0 && (
        <div className="card">
          <div className="card-head">
            <div>
              <div className="card-title">Tool cost matrix</div>
              <div className="card-desc">
                Top {toolMatrix.length} tools by total bytes returned — the red segment is each tool's own failure
                rate
              </div>
            </div>
          </div>
          <div className="card-pad">
            <div style={{ height: Math.max(200, toolMatrix.length * 34) }}>
              <ResponsiveContainer width="100%" height="100%">
                <BarChart
                  data={toolMatrix}
                  layout="vertical"
                  margin={{ top: 8, right: 24, bottom: 0, left: 8 }}
                  barCategoryGap="25%"
                >
                  <CartesianGrid horizontal={false} stroke="var(--color-wire)" strokeDasharray="3 4" />
                  <XAxis
                    type="number"
                    stroke="var(--color-wire)"
                    tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                    tickFormatter={(v) => formatBytes(Number(v))}
                  />
                  <YAxis
                    type="category"
                    dataKey="label"
                    stroke="var(--color-wire)"
                    tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                    width={150}
                  />
                  <Tooltip
                    contentStyle={tooltipStyle}
                    labelStyle={{ color: "var(--color-ink)" }}
                    formatter={(_value, name, item) =>
                      name === "failed"
                        ? [`${item.payload.failureRatePct.toFixed(1)}% failure rate`, "failures"]
                        : [formatBytes(item.payload.totalBytes), "total bytes"]
                    }
                  />
                  <Bar dataKey="healthyBytes" name="healthy" stackId="bytes" fill="var(--color-chart)" radius={0} />
                  <Bar dataKey="failedBytes" name="failed" stackId="bytes" fill="var(--color-fault)" radius={[0, 3, 3, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
          <div className="card-foot">The longest bars with a visible red segment are your expensive, failure-prone tools.</div>
        </div>
      )}

      {ancillarySpend.length > 0 && (
        <div className="card p-4.5">
          <div className="flex items-center justify-between gap-4">
            <div>
              <div className="card-title">Background call model mix</div>
              <div className="card-desc">
                {ancillaryModel ?? "A non-Haiku model"} is doing Haiku's work on short backstage calls (title/summary/
                suggestion generation)
              </div>
            </div>
            <div className="kpi-value shrink-0">{formatCostUsd(ancillaryTotalUSD)}</div>
          </div>
        </div>
      )}

      <div className="card">
        <div className="card-head">
          <div>
            <div className="card-title">Sessions</div>
            <div className="card-desc">Sortable across every recorded metric</div>
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="min-w-[900px]">
            <thead>
              <tr>
                <th>Session</th>
                {COLUMNS.map((col) => (
                  <th
                    key={col.key}
                    onClick={() => toggleSort(col.key)}
                    className={`cursor-pointer select-none hover:text-ink ${col.key === "started_at" ? "" : "num"}`}
                    aria-sort={sortKey === col.key ? (sortDir === "asc" ? "ascending" : "descending") : undefined}
                  >
                    {col.label}
                    {sortKey === col.key && (sortDir === "asc" ? " ↑" : " ↓")}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sorted.map((s) => (
                <tr key={s.session_id} className="row-link" onClick={() => navigate(`/trace?session=${s.session_id}`)}>
                  <td>
                    <span className="cell-truncate" title={s.session_id}>
                      {sessionLabels.get(s.session_id) ?? <span className="code">{s.session_id.slice(0, 8)}</span>}
                    </span>
                  </td>
                  <td className="dim">{formatTimestamp(s.started_at)}</td>
                  <td className="num">{formatDurationMs(s.duration_ms)}</td>
                  <td className="num">{formatDurationMs(((s.active_time_user_s ?? 0) + (s.active_time_cli_s ?? 0)) * 1000)}</td>
                  <td className="num strong">{formatCostUsd(s.total_cost_usd)}</td>
                  <td className="num">{(s.total_tokens ?? 0).toLocaleString()}</td>
                  <td className="num">{s.tool_call_count ?? 0}</td>
                  <td className="num">{s.lines_added ?? 0}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {sorted.length === 0 && <div className="empty">No sessions recorded yet.</div>}
        </div>
        <div className="card-foot">Wall clock includes idle time; active work is user plus CLI engagement — the gap is the insight.</div>
      </div>

      <div className="card">
        <div className="card-head">
          <div className="flex items-center gap-3.5">
            <div>
              <div className="card-title">Tools &amp; models</div>
              <div className="card-desc">Call volume, success rate, and latency</div>
            </div>
          </div>
          <div className="seg">
            <button type="button" className={`seg-btn ${toolsTab === "tools" ? "seg-btn-active" : ""}`} onClick={() => setToolsTab("tools")}>
              Tools
            </button>
            <button type="button" className={`seg-btn ${toolsTab === "models" ? "seg-btn-active" : ""}`} onClick={() => setToolsTab("models")}>
              Models
            </button>
          </div>
        </div>

        {toolsTab === "tools" && (
          <>
            <div className="flex flex-wrap gap-2 px-5 pt-4">
              {(["all", "native", "mcp", "failing", "slow"] as ToolsFilter[]).map((f) => (
                <button
                  key={f}
                  type="button"
                  className={`chip capitalize ${toolsFilter === f ? "chip-active" : ""}`}
                  onClick={() => {
                    setToolsFilter(f);
                    setToolsShowAll(false);
                  }}
                >
                  {f === "mcp" ? "MCP" : f}
                </button>
              ))}
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
                  {visibleUsage.map((r) => {
                    const total = r.successCount + r.errorCount;
                    const rate = total > 0 ? Math.round((r.successCount / total) * 100) : null;
                    const slow = (r.avgDurationMs ?? 0) >= 1000;
                    return (
                      <tr key={r.label}>
                        <td><span className="code">{r.label}</span></td>
                        <td className="num">{r.totalCalls}</td>
                        <td className={`num ${rate != null && rate < 90 ? "fault" : "good"}`}>{rate == null ? "—" : `${rate}%`}</td>
                        <td className={`num ${slow ? "warn" : ""}`}>{formatDurationMs(r.avgDurationMs)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
              {filteredUsage.length === 0 && <div className="empty">No tool calls match this filter.</div>}
            </div>
            {filteredUsage.length > TOOLS_TABLE_PAGE_SIZE && (
              <div className="px-5 py-3">
                <button type="button" className="link-more" onClick={() => setToolsShowAll((v) => !v)}>
                  {toolsShowAll ? "Show top 15" : `Show all ${filteredUsage.length}`}
                </button>
              </div>
            )}
            {deadServerCount > 0 && (
              <div className="card-foot">
                {deadServerCount} connected MCP server{deadServerCount === 1 ? "" : "s"} never had a tool call — see the Fix page.
              </div>
            )}
          </>
        )}

        {toolsTab === "models" && (
          <div className="overflow-x-auto">
            <table>
              <thead>
                <tr>
                  <th>Model</th>
                  <th className="num">Effort</th>
                  <th className="num">Speed</th>
                  <th className="num">Requests</th>
                  <th className="num">Error rate</th>
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
                      <td className={`num ${errPct != null && errPct > 1 ? "warn" : ""}`}>{errPct == null ? "—" : `${errPct}%`}</td>
                      <td className="num">{formatDurationMs(m.p50_duration_ms)}</td>
                      <td className="num">{formatDurationMs(m.p95_duration_ms)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {modelPerf.length === 0 && <div className="empty">No model activity recorded yet.</div>}
          </div>
        )}
      </div>
    </PageShell>
  );
}
