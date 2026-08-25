import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { Link, useNavigate } from "react-router-dom";
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
import { InsightBanner } from "../components/InsightBanner";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { Toolbar, ExportReportButton } from "../components/Toolbar";
import { type TimeRange } from "../lib/timeRange";
import {
  listSessions,
  getSession,
  getInsights,
  getView,
  type Insight,
  type Session,
  type PromptEfficiency,
  type WastedSpend,
} from "../lib/api";
import { formatCostUsd, formatDurationMs, formatTimestamp } from "../lib/format";

type SortKey =
  | "started_at"
  | "duration_ms"
  | "active_time_ms"
  | "total_cost_usd"
  | "total_tokens"
  | "tool_call_count"
  | "commit_count"
  | "pull_request_count"
  | "lines_added";

const COLUMNS: { key: SortKey; label: string }[] = [
  { key: "started_at", label: "Started" },
  { key: "duration_ms", label: "Wall clock" },
  { key: "active_time_ms", label: "Active work" },
  { key: "total_cost_usd", label: "Cost" },
  { key: "total_tokens", label: "Tokens" },
  { key: "tool_call_count", label: "Tool calls" },
  { key: "commit_count", label: "Commits" },
  { key: "pull_request_count", label: "PRs" },
  { key: "lines_added", label: "+Lines" },
];

const CONCENTRATION_TOP_N = 9;
const WASTE_BAR_TOP_N = 9;

type ConcentrationRow = {
  session_id: string;
  conversations: number;
  cost: number;
  pctOfRange: number;
  cumulativePct: number;
  costliestConversation: string;
};

async function fetchConcentration(top: Session[]): Promise<ConcentrationRow[]> {
  const details = await Promise.all(
    top.map((s) =>
      Promise.all([
        getSession(s.session_id),
        getView<PromptEfficiency>("prompt_efficiency", { session_id: s.session_id, limit: "500" }),
      ]),
    ),
  );

  const totalCost = top.reduce((sum, s) => sum + (s.total_cost_usd ?? 0), 0);
  let cumulative = 0;

  return top.map((s, idx) => {
    const [detail, promptRows] = details[idx];
    const costByPrompt = new Map(promptRows.map((r) => [r.prompt_id, r.total_cost_usd]));
    let costliestText = "—";
    let costliestCost = -1;
    for (const interaction of detail.interactions) {
      const cost = costByPrompt.get(interaction.prompt_id) ?? 0;
      if (cost > costliestCost) {
        costliestCost = cost;
        costliestText =
          interaction.command_name ?? (interaction.prompt_text || "—").slice(0, 90);
      }
    }
    const cost = s.total_cost_usd ?? 0;
    const pctOfRange = totalCost > 0 ? (cost / totalCost) * 100 : 0;
    cumulative += pctOfRange;
    return {
      session_id: s.session_id,
      conversations: detail.interactions.length,
      cost,
      pctOfRange,
      cumulativePct: cumulative,
      costliestConversation: detail.interactions.length ? costliestText : "—",
    };
  });
}

export default function Sessions() {
  const navigate = useNavigate();
  const [view, setView] = useState<"sessions" | "conversations">("sessions");
  const [range, setRange] = useState<TimeRange>("30d");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [concentration, setConcentration] = useState<ConcentrationRow[]>([]);
  const [wastePerSession, setWastePerSession] = useState<{ session_id: string; cost: number }[]>([]);
  const [insights, setInsights] = useState<Insight[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [sortKey, setSortKey] = useState<SortKey>("total_cost_usd");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");

  const [conversations, setConversations] = useState<PromptEfficiency[]>([]);

  useEffect(() => {
    listSessions(200, 0, range)
      .then(setSessions)
      .catch((e) => setError(String(e)));
  }, [range]);

  useEffect(() => {
    getInsights(range || undefined)
      .then((all) => setInsights(all.filter((i) => i.category === "sessions")))
      .catch(() => setInsights([]));
  }, [range]);

  useEffect(() => {
    if (sessions.length === 0) {
      setConcentration([]);
      return;
    }
    const top = [...sessions]
      .sort((a, b) => (b.total_cost_usd ?? 0) - (a.total_cost_usd ?? 0))
      .slice(0, CONCENTRATION_TOP_N);
    fetchConcentration(top)
      .then(setConcentration)
      .catch((e) => setError(String(e)));
  }, [sessions]);

  useEffect(() => {
    getView<WastedSpend>("wasted_spend", { limit: "1000", ...(range ? { since: range } : {}) })
      .then((rows) => {
        const bySession = new Map<string, number>();
        for (const r of rows) bySession.set(r.session_id, (bySession.get(r.session_id) ?? 0) + r.wasted_cost_usd);
        setWastePerSession(
          [...bySession.entries()]
            .map(([session_id, cost]) => ({ session_id, cost }))
            .sort((a, b) => b.cost - a.cost)
            .slice(0, WASTE_BAR_TOP_N),
        );
      })
      .catch(() => setWastePerSession([]));
  }, [range]);

  useEffect(() => {
    if (view !== "conversations") return;
    getView<PromptEfficiency>("prompt_efficiency", {
      limit: "200",
      ...(range ? { since: range } : {}),
    })
      .then((r) => setConversations([...r].sort((a, b) => b.total_cost_usd - a.total_cost_usd)))
      .catch((e) => setError(String(e)));
  }, [view, range]);

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

  const underThreshold = useMemo(
    () => sessions.filter((s) => (s.duration_ms ?? 0) < 3000),
    [sessions],
  );

  function toggleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  }

  const maxWaste = Math.max(1, ...wastePerSession.map((w) => w.cost));
  const wasteScaleMax = Math.ceil(maxWaste / 3) * 3 || 3;

  const scatterData = useMemo(
    () =>
      conversations
        .filter((r) => r.prompt_length != null)
        .map((r) => ({
          x: r.prompt_length,
          y: r.llm_turn_count,
          cost: r.total_cost_usd,
          prompt_id: r.prompt_id,
        })),
    [conversations],
  );

  return (
    <PageShell
      title="Sessions"
      subtitle="Every agent run recorded locally, and the conversations inside each one."
    >
      <Toolbar
        left={
          <>
            <TimeRangeSelect value={range} onChange={setRange} />
            <div className="seg">
              <button
                type="button"
                className={`seg-btn ${view === "sessions" ? "seg-btn-active" : ""}`}
                onClick={() => setView("sessions")}
              >
                Sessions
              </button>
              <button
                type="button"
                className={`seg-btn ${view === "conversations" ? "seg-btn-active" : ""}`}
                onClick={() => setView("conversations")}
              >
                Conversations
              </button>
            </div>
          </>
        }
        right={<ExportReportButton />}
      />

      {error && <p className="text-sm text-fault">{error}</p>}

      {insights.map((i) => (
        <InsightBanner key={i.id} insight={i} />
      ))}

      {view === "sessions" && (
        <>
          {wastePerSession.length > 0 && (
            <div className="card card-pad">
              <div className="card-title">Wasted spend per session</div>
              <div className="card-desc">Cost of conversations containing a rejected call</div>
              <div
                className="mt-5 flex flex-col gap-2"
                style={{ "--label-w": "96px" } as CSSProperties}
              >
                {wastePerSession.map((w) => (
                  <div key={w.session_id} className="hbar">
                    <span className="hbar-label">{w.session_id.slice(0, 8)}</span>
                    <div className="hbar-val" style={{ width: `${(w.cost / wasteScaleMax) * 100}%` }} />
                  </div>
                ))}
                <div className="hbar">
                  <span />
                  <div className="hbar-scale">
                    <span>0</span>
                    <span>{Math.round(wasteScaleMax / 3)}</span>
                    <span>{Math.round((wasteScaleMax / 3) * 2)}</span>
                    <span>{wasteScaleMax}</span>
                  </div>
                </div>
              </div>
            </div>
          )}

          {concentration.length > 0 && (
            <div className="card">
              <div className="card-head">
                <div className="flex items-center gap-3.5">
                  <span className="card-icon">
                    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                      <path d="M12 3 3 8l9 5 9-5-9-5zM3 13l9 5 9-5M3 17.5l9 5 9-5" />
                    </svg>
                  </span>
                  <div>
                    <div className="card-title">Cost concentration</div>
                    <div className="card-desc">Which session cost most, and which conversation inside it</div>
                  </div>
                </div>
              </div>
              <div className="overflow-x-auto">
                <table>
                  <thead>
                    <tr>
                      <th>Session</th>
                      <th className="num">Conversations</th>
                      <th className="num">Cost</th>
                      <th className="num">% of range</th>
                      <th className="num">Cumulative %</th>
                      <th>Costliest conversation</th>
                    </tr>
                  </thead>
                  <tbody>
                    {concentration.map((r) => (
                      <tr
                        key={r.session_id}
                        className="row-link"
                        onClick={() => navigate(`/waterfall?session=${r.session_id}`)}
                      >
                        <td><span className="code">{r.session_id.slice(0, 8)}</span></td>
                        <td className="num">{r.conversations}</td>
                        <td className="num strong">{formatCostUsd(r.cost)}</td>
                        <td className="num">{r.pctOfRange.toFixed(1)}%</td>
                        <td className="num dim">{r.cumulativePct.toFixed(1)}%</td>
                        <td className="cell-truncate dim">{r.costliestConversation}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          <div className="card">
            <div className="card-head">
              <div className="flex items-center gap-3.5">
                <span className="card-icon">
                  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                    <path d="M3 12h4l3-7 4 14 3-7h4" />
                  </svg>
                </span>
                <div>
                  <div className="card-title">Sessions</div>
                  <div className="card-desc">Sortable across every recorded metric</div>
                </div>
              </div>
            </div>
            <div className="overflow-x-auto">
              <table className="min-w-[1020px]">
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
                    <tr
                      key={s.session_id}
                      className="row-link"
                      onClick={() => navigate(`/waterfall?session=${s.session_id}`)}
                    >
                      <td><span className="code">{s.session_id.slice(0, 8)}</span></td>
                      <td className="dim">{formatTimestamp(s.started_at)}</td>
                      <td className="num">{formatDurationMs(s.duration_ms)}</td>
                      <td className="num">
                        {formatDurationMs(((s.active_time_user_s ?? 0) + (s.active_time_cli_s ?? 0)) * 1000)}
                      </td>
                      <td className="num strong">{formatCostUsd(s.total_cost_usd)}</td>
                      <td className="num">{(s.total_tokens ?? 0).toLocaleString()}</td>
                      <td className="num">{s.tool_call_count ?? 0}</td>
                      <td className="num dim">{s.commit_count ?? 0}</td>
                      <td className="num dim">{s.pull_request_count || "—"}</td>
                      <td className="num">{s.lines_added ?? 0}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {sorted.length === 0 && (
                <div className="empty">No sessions recorded yet.</div>
              )}
            </div>
            {underThreshold.length > 0 && (
              <div className="card-foot">
                {underThreshold.length} session{underThreshold.length === 1 ? "" : "s"} under 3s
              </div>
            )}
          </div>

          <div className="flex flex-wrap gap-2">
            <span className="chip">Wall clock includes idle time</span>
            <span className="chip">Active work is user plus CLI engagement</span>
          </div>
        </>
      )}

      {view === "conversations" && (
        <>
          <div className="card card-pad">
            <div className="card-title">Prompt length vs. turn count</div>
            <div className="card-desc">Bubble size is cost</div>
            <div className="mt-4 h-64">
              <ResponsiveContainer width="100%" height="100%">
                <ScatterChart margin={{ top: 8, right: 16, bottom: 8, left: 0 }}>
                  <CartesianGrid stroke="var(--color-wire)" strokeDasharray="3 3" />
                  <XAxis
                    type="number"
                    dataKey="x"
                    name="prompt length"
                    stroke="var(--color-wire-2)"
                    tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                  />
                  <YAxis
                    type="number"
                    dataKey="y"
                    name="turn count"
                    stroke="var(--color-wire-2)"
                    tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                  />
                  <ZAxis type="number" dataKey="cost" range={[20, 200]} name="cost" />
                  <Tooltip
                    cursor={{ strokeDasharray: "3 3" }}
                    contentStyle={{
                      background: "var(--color-panel-2)",
                      border: "1px solid var(--color-wire-2)",
                      borderRadius: 6,
                      fontSize: 12,
                    }}
                    formatter={(value, name) => [String(value), name]}
                  />
                  <Scatter data={scatterData} fill="var(--color-signal)" fillOpacity={0.7} />
                </ScatterChart>
              </ResponsiveContainer>
            </div>
          </div>

          <div className="card">
            <div className="card-head">
              <div>
                <div className="card-title">Conversations</div>
                <div className="card-desc">Prompt-level cost, tokens, and turnaround — sorted by cost, descending</div>
              </div>
            </div>
            <div className="overflow-x-auto">
              <table className="min-w-[900px]">
                <thead>
                  <tr>
                    <th>Prompt</th>
                    <th className="num">Cost</th>
                    <th className="num">Tokens</th>
                    <th className="num">Turns</th>
                    <th className="num">Tool calls</th>
                    <th className="num">Rejected</th>
                    <th className="num">Errors</th>
                    <th className="num">Cache hit</th>
                    <th className="num">Blocked</th>
                  </tr>
                </thead>
                <tbody>
                  {conversations.map((r) => (
                    <tr key={r.prompt_id}>
                      <td>
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
                      <td className="num strong">{formatCostUsd(r.total_cost_usd)}</td>
                      <td className="num">{r.total_tokens.toLocaleString()}</td>
                      <td className="num">{r.llm_turn_count}</td>
                      <td className="num">{r.tool_call_count}</td>
                      <td className="num fault">{r.rejected_count}</td>
                      <td className="num fault">{r.tool_error_count}</td>
                      <td className="num">
                        {r.cache_hit_ratio == null ? "—" : `${Math.round(r.cache_hit_ratio * 100)}%`}
                      </td>
                      <td className="num">
                        {r.blocked_on_user_ms > 0 ? formatDurationMs(r.blocked_on_user_ms) : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {conversations.length === 0 && (
                <div className="empty">No interactions recorded yet.</div>
              )}
            </div>
          </div>
        </>
      )}
    </PageShell>
  );
}
