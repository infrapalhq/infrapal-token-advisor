import { useEffect, useMemo, useState, type CSSProperties } from "react";
import {
  AreaChart,
  Area,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { PageShell } from "../components/PageShell";
import { Toolbar } from "../components/Toolbar";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { type TimeRange } from "../lib/timeRange";
import type { PromptEfficiency, McpInventory } from "../lib/api";
import {
  getViewDev,
  getTimeseriesDev,
  listSessionsDev,
  type Point,
  type SessionLite,
  type ToolPayload,
  type RetryWaste,
} from "../lib/apiOptimize";
import { formatCostUsd, formatTokenCount } from "../lib/format";

type Composed = {
  bucket: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheCreation: number;
};

function mergeTokenSeries(
  input: Point[],
  output: Point[],
  cacheRead: Point[],
  cacheCreation: Point[],
): Composed[] {
  const buckets = new Map<string, Composed>();
  const put = (points: Point[], key: keyof Omit<Composed, "bucket">) => {
    for (const p of points) {
      const row = buckets.get(p.bucket) ?? {
        bucket: p.bucket,
        input: 0,
        output: 0,
        cacheRead: 0,
        cacheCreation: 0,
      };
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

function formatBytes(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return "—";
  if (n < 1024) return `${Math.round(n)} B`;
  const kb = n / 1024;
  if (kb < 1024) return `${kb.toFixed(1)} KB`;
  return `${(kb / 1024).toFixed(2)} MB`;
}

function toolLabel(row: ToolPayload): string {
  if (row.mcp_server_name) return `${row.mcp_server_name} · ${row.mcp_tool_name ?? "?"}`;
  return row.tool_name;
}

function Kpi({ label, value, note, fault }: { label: string; value: string; note: string; fault?: boolean }) {
  return (
    <div className={`card p-4.5 ${fault ? "border-fault/40 bg-fault/5" : ""}`}>
      <div className="kpi-label">{label}</div>
      <div className="kpi-value">{value}</div>
      <div className="kpi-note">{note}</div>
    </div>
  );
}

const tooltipStyle = {
  background: "var(--color-panel-2)",
  border: "1px solid var(--color-wire-2)",
  borderRadius: 6,
  fontSize: 12,
};

export default function Optimize() {
  const [range, setRange] = useState<TimeRange>("30d");
  const [tokenSeries, setTokenSeries] = useState<Composed[]>([]);
  const [toolPayload, setToolPayload] = useState<ToolPayload[]>([]);
  const [mcpInventory, setMcpInventory] = useState<McpInventory[]>([]);
  const [promptEff, setPromptEff] = useState<PromptEfficiency[]>([]);
  const [retryWaste, setRetryWaste] = useState<RetryWaste[]>([]);
  const [sessions, setSessions] = useState<SessionLite[]>([]);
  const [selectedSession, setSelectedSession] = useState<string>("");
  const [sessionRows, setSessionRows] = useState<PromptEfficiency[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getTimeseriesDev("tokens_input", "day", range || "90d"),
      getTimeseriesDev("tokens_output", "day", range || "90d"),
      getTimeseriesDev("tokens_cache_read", "day", range || "90d"),
      getTimeseriesDev("tokens_cache_creation", "day", range || "90d"),
    ])
      .then(([i, o, cr, cc]) => setTokenSeries(mergeTokenSeries(i, o, cr, cc)))
      .catch((e) => setError(String(e)));

    getViewDev<PromptEfficiency>("prompt_efficiency", {
      limit: "2000",
      ...(range ? { since: range } : {}),
    })
      .then(setPromptEff)
      .catch((e) => setError(String(e)));

    getViewDev<RetryWaste>("retry_waste", {
      limit: "1000",
      ...(range ? { since: range } : {}),
    })
      .then(setRetryWaste)
      .catch((e) => setError(String(e)));
  }, [range]);

  useEffect(() => {
    getViewDev<ToolPayload>("tool_payload", { limit: "500" })
      .then((rows) => setToolPayload([...rows].sort((a, b) => b.total_result_bytes - a.total_result_bytes)))
      .catch((e) => setError(String(e)));

    getViewDev<McpInventory>("mcp_inventory", { limit: "200" })
      .then((rows) => setMcpInventory([...rows].sort((a, b) => b.connection_count - a.connection_count)))
      .catch((e) => setError(String(e)));

    listSessionsDev(100)
      .then((rows) => {
        setSessions(rows);
        const richest = [...rows].sort((a, b) => (b.tool_call_count ?? 0) - (a.tool_call_count ?? 0))[0];
        if (richest) setSelectedSession(richest.session_id);
      })
      .catch((e) => setError(String(e)));
  }, []);

  useEffect(() => {
    if (!selectedSession) return;
    getViewDev<PromptEfficiency>("prompt_efficiency", { session_id: selectedSession, limit: "500" })
      .then((rows) => setSessionRows([...rows].sort((a, b) => a.started_at - b.started_at)))
      .catch((e) => setError(String(e)));
  }, [selectedSession]);

  // Stat 1 + chart 3: cache hit ratio, weighted across the whole range
  // rather than averaged per-bucket (a day with one huge request shouldn't
  // count the same as a day with one tiny one).
  const cacheSeries = useMemo(
    () =>
      tokenSeries.map((d) => ({
        bucket: d.bucket,
        ratio: d.cacheRead + d.cacheCreation > 0 ? (d.cacheRead / (d.cacheRead + d.cacheCreation)) * 100 : null,
      })),
    [tokenSeries],
  );
  const cacheHitRatioAvg = useMemo(() => {
    const totalRead = tokenSeries.reduce((s, d) => s + d.cacheRead, 0);
    const totalCreate = tokenSeries.reduce((s, d) => s + d.cacheCreation, 0);
    return totalRead + totalCreate > 0 ? (totalRead / (totalRead + totalCreate)) * 100 : null;
  }, [tokenSeries]);

  // Stat 2 + chart 2: payload size by tool. result_tokens is reserved for
  // installs where Claude Code populates it directly — it's 0 here, so
  // bytes are the honest signal for which tools carry the most content
  // back into context.
  const topToolPayload = useMemo(() => toolPayload.slice(0, 10), [toolPayload]);
  const totalToolCalls = useMemo(() => toolPayload.reduce((s, r) => s + r.call_count, 0), [toolPayload]);
  const totalResultBytes = useMemo(() => toolPayload.reduce((s, r) => s + r.total_result_bytes, 0), [toolPayload]);
  const avgResultBytes = totalToolCalls > 0 ? totalResultBytes / totalToolCalls : null;
  const hasResultTokens = useMemo(() => toolPayload.some((r) => r.total_result_tokens > 0), [toolPayload]);
  const maxToolBytes = Math.max(1, ...topToolPayload.map((r) => r.total_result_bytes));

  // Stat 3: prompts that hit compaction or the token ceiling.
  const flaggedPct = useMemo(() => {
    if (promptEff.length === 0) return null;
    const flagged = promptEff.filter((r) => r.triggered_compaction || r.hit_max_tokens).length;
    return (flagged / promptEff.length) * 100;
  }, [promptEff]);

  // Stat 4: cost/tokens spent on requests that needed a retry.
  const retryTotals = useMemo(
    () => ({
      requests: retryWaste.reduce((s, r) => s + r.retried_requests, 0),
      cost: retryWaste.reduce((s, r) => s + r.retried_cost_usd, 0),
      tokens: retryWaste.reduce((s, r) => s + r.retried_tokens, 0),
    }),
    [retryWaste],
  );

  // Chart 4 + stat 5: MCP servers whose tool definitions ride along on
  // every request despite never being called.
  const deadServers = useMemo(
    () => mcpInventory.filter((s) => s.connection_count > 0 && s.tool_call_count === 0),
    [mcpInventory],
  );
  const maxMcpCalls = Math.max(1, ...mcpInventory.map((s) => s.tool_call_count));

  const sessionOptions = useMemo(
    () => [...sessions].sort((a, b) => (b.tool_call_count ?? 0) - (a.tool_call_count ?? 0)).slice(0, 30),
    [sessions],
  );
  const sessionChartData = useMemo(
    () =>
      sessionRows.map((r, idx) => ({
        turn: idx + 1,
        total_tokens: r.total_tokens,
        flagged: Boolean(r.triggered_compaction || r.hit_max_tokens),
      })),
    [sessionRows],
  );

  return (
    <PageShell
      title="Optimize"
      subtitle="Five signals for spending fewer tokens per session — built from this install's own telemetry."
    >
      <Toolbar left={<TimeRangeSelect value={range} onChange={setRange} allowAllTime={false} />} />

      {error && <p className="text-sm text-fault">{error}</p>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
        <Kpi
          label="Cache hit ratio"
          value={cacheHitRatioAvg == null ? "—" : `${Math.round(cacheHitRatioAvg)}%`}
          note="cache_read ÷ (cache_read + cache_creation), this range"
        />
        <Kpi
          label="Avg tool payload"
          value={formatBytes(avgResultBytes)}
          note={
            hasResultTokens
              ? "avg result size across every tool call"
              : "result_tokens isn't populated by this install — showing bytes"
          }
        />
        <Kpi
          label="Compaction / max-tokens"
          value={flaggedPct == null ? "—" : `${flaggedPct.toFixed(1)}%`}
          note={`of ${promptEff.length} prompt${promptEff.length === 1 ? "" : "s"} in range`}
          fault={Boolean(flaggedPct && flaggedPct > 10)}
        />
        <Kpi
          label="Retry waste"
          value={formatCostUsd(retryTotals.cost)}
          note={`${retryTotals.requests} retried request${retryTotals.requests === 1 ? "" : "s"} · ${formatTokenCount(retryTotals.tokens)} tok`}
        />
        <Kpi
          label="Dead MCP servers"
          value={String(deadServers.length)}
          note="connected, never called — tax on every request"
          fault={deadServers.length > 0}
        />
      </div>

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
              <AreaChart data={tokenSeries} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
                <defs>
                  <linearGradient id="grad-opt-input" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-cat-llm)" stopOpacity={0.55} />
                    <stop offset="100%" stopColor="var(--color-cat-llm)" stopOpacity={0.08} />
                  </linearGradient>
                  <linearGradient id="grad-opt-output" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-signal)" stopOpacity={0.55} />
                    <stop offset="100%" stopColor="var(--color-signal)" stopOpacity={0.08} />
                  </linearGradient>
                  <linearGradient id="grad-opt-cread" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-good)" stopOpacity={0.55} />
                    <stop offset="100%" stopColor="var(--color-good)" stopOpacity={0.08} />
                  </linearGradient>
                  <linearGradient id="grad-opt-ccreate" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-warn)" stopOpacity={0.55} />
                    <stop offset="100%" stopColor="var(--color-warn)" stopOpacity={0.08} />
                  </linearGradient>
                </defs>
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
                <Area type="monotone" dataKey="cacheCreation" name="cache creation" stackId="tok" stroke="var(--color-warn)" fill="url(#grad-opt-ccreate)" strokeWidth={1.4} isAnimationActive={false} />
                <Area type="monotone" dataKey="cacheRead" name="cache read" stackId="tok" stroke="var(--color-good)" fill="url(#grad-opt-cread)" strokeWidth={1.4} isAnimationActive={false} />
                <Area type="monotone" dataKey="input" name="input" stackId="tok" stroke="var(--color-cat-llm)" fill="url(#grad-opt-input)" strokeWidth={1.4} isAnimationActive={false} />
                <Area type="monotone" dataKey="output" name="output" stackId="tok" stroke="var(--color-signal)" fill="url(#grad-opt-output)" strokeWidth={1.4} isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
        <div className="card-foot">
          Green (cache read) is cheap reuse; amber (cache creation) is a full rebuild. A range where amber
          crowds out green is a range where the cache kept getting invalidated.
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <div className="card">
          <div className="card-head">
            <div>
              <div className="card-title">Payload returned by tool</div>
              <div className="card-desc">Top {topToolPayload.length} by bytes sent back into context, all time</div>
            </div>
          </div>
          <div className="card-pad">
            <div className="flex flex-col gap-2" style={{ "--label-w": "150px" } as CSSProperties}>
              {topToolPayload.map((r) => (
                <div key={`${r.tool_name}-${r.mcp_server_name}-${r.mcp_tool_name}`} className="hbar">
                  <span className="hbar-label" title={toolLabel(r)}>{toolLabel(r)}</span>
                  <div className="hbar-val" style={{ width: `${(r.total_result_bytes / maxToolBytes) * 100}%` }} />
                </div>
              ))}
              {topToolPayload.length === 0 && <div className="empty">No tool calls recorded yet.</div>}
            </div>
          </div>
          <div className="card-foot">
            {formatBytes(totalResultBytes)} returned across {totalToolCalls} calls
            {!hasResultTokens && " — exact token counts aren't populated by this install"}.
          </div>
        </div>

        <div className="card">
          <div className="card-head">
            <div>
              <div className="card-title">MCP tool-definition tax</div>
              <div className="card-desc">Connections vs. calls actually made, all time</div>
            </div>
          </div>
          <div className="card-pad">
            <div className="flex flex-col gap-2" style={{ "--label-w": "150px" } as CSSProperties}>
              {mcpInventory.map((s) => (
                <div key={s.server_name} className="hbar">
                  <span className="hbar-label" title={s.server_name}>{s.server_name}</span>
                  {s.tool_call_count === 0 ? (
                    <div className="flex h-5 items-center gap-2">
                      <div className="h-5 w-1.5 rounded-sm bg-fault" />
                      <span className="pill pill-warn">connected {s.connection_count}× · 0 calls</span>
                    </div>
                  ) : (
                    <div className="hbar-val" style={{ width: `${(s.tool_call_count / maxMcpCalls) * 100}%` }} />
                  )}
                </div>
              ))}
              {mcpInventory.length === 0 && <div className="empty">No MCP servers seen yet.</div>}
            </div>
          </div>
          {deadServers.length > 0 && (
            <div className="card-foot">
              {deadServers.length} of {mcpInventory.length} connected servers never had a single tool call —
              their definitions still ride along on every request.
            </div>
          )}
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <div>
            <div className="card-title">Cache hit ratio trend</div>
            <div className="card-desc">Daily cache_read share of all cacheable tokens</div>
          </div>
        </div>
        <div className="card-pad">
          <div className="h-48">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={cacheSeries} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
                <CartesianGrid vertical={false} stroke="var(--color-wire)" strokeDasharray="3 4" />
                <XAxis dataKey="bucket" stroke="var(--color-wire)" tick={{ fill: "var(--color-ink)", fontSize: 11 }} />
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
                  formatter={(v) => [v == null ? "—" : `${Number(v).toFixed(1)}%`, "cache hit"]}
                />
                <Line type="monotone" dataKey="ratio" stroke="var(--color-good)" strokeWidth={2} dot={false} isAnimationActive={false} connectNulls />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <div>
            <div className="card-title">Context growth within a session</div>
            <div className="card-desc">Total tokens per turn — red dots mark a compaction or max-tokens event</div>
          </div>
          <select
            className="select w-[280px]"
            value={selectedSession}
            onChange={(e) => setSelectedSession(e.target.value)}
          >
            {sessionOptions.map((s) => (
              <option key={s.session_id} value={s.session_id}>
                {s.session_id.slice(0, 8)} · {s.tool_call_count ?? 0} calls · {formatTokenCount(s.total_tokens)} tok
              </option>
            ))}
          </select>
        </div>
        <div className="card-pad">
          <div className="h-56">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={sessionChartData} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
                <CartesianGrid vertical={false} stroke="var(--color-wire)" strokeDasharray="3 4" />
                <XAxis dataKey="turn" stroke="var(--color-wire)" tick={{ fill: "var(--color-ink)", fontSize: 11 }} label={{ value: "turn", position: "insideBottom", offset: -2, fill: "var(--color-ink)", fontSize: 11 }} />
                <YAxis
                  stroke="var(--color-wire)"
                  tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                  tickFormatter={(v) => formatTokenCount(Number(v))}
                  width={48}
                />
                <Tooltip
                  contentStyle={tooltipStyle}
                  labelStyle={{ color: "var(--color-ink)" }}
                  formatter={(v) => [formatTokenCount(Number(v ?? 0)), "tokens"]}
                />
                <Line
                  type="monotone"
                  dataKey="total_tokens"
                  stroke="var(--color-chart)"
                  strokeWidth={2}
                  isAnimationActive={false}
                  dot={(props: { cx?: number; cy?: number; payload?: { flagged: boolean }; index?: number }) => {
                    const { cx, cy, payload, index } = props;
                    if (cx == null || cy == null) return <g key={index} />;
                    const flagged = Boolean(payload?.flagged);
                    return (
                      <circle
                        key={index}
                        cx={cx}
                        cy={cy}
                        r={flagged ? 5 : 2.5}
                        fill={flagged ? "var(--color-fault)" : "var(--color-chart)"}
                      />
                    );
                  }}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
          {sessionChartData.length === 0 && <div className="empty">Pick a session to see its context grow turn by turn.</div>}
        </div>
      </div>
    </PageShell>
  );
}
