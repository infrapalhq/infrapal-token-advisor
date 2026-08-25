import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  AreaChart,
  Area,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { PageShell } from "../components/PageShell";
import { InsightBanner } from "../components/InsightBanner";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { Toolbar, ExportReportButton } from "../components/Toolbar";
import { timeRangeLabel, type TimeRange } from "../lib/timeRange";
import {
  listSessions,
  getSession,
  getTimeseries,
  getInsights,
  getView,
  type Insight,
  type SessionInteraction,
  type WastedSpend,
  type PromptEfficiency,
} from "../lib/api";
import {
  formatCostUsd,
  formatTimestamp,
  summarizePromptText,
} from "../lib/format";

type Point = { bucket: string; value: number };
type RecentInteraction = SessionInteraction & { session_id: string; cost_usd: number };

function todayValue(points: Point[]): number {
  return points.length ? points[points.length - 1].value : 0;
}

function Sparkline({
  points,
  color,
  gradientId,
  formatValue = (v: number) => v.toLocaleString(),
}: {
  points: Point[];
  color: string;
  gradientId: string;
  formatValue?: (v: number) => string;
}) {
  if (points.length < 2) return <div className="mt-3 h-9" />;
  return (
    <div className="mt-3 h-9">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={points} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.3} />
              <stop offset="100%" stopColor={color} stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <Tooltip
            cursor={{ stroke: "var(--color-wire-2)", strokeWidth: 1 }}
            contentStyle={{
              background: "var(--color-panel-2)",
              border: "1px solid var(--color-wire-2)",
              borderRadius: 6,
              fontSize: 11,
              padding: "4px 8px",
            }}
            labelStyle={{ color: "var(--color-ink)" }}
            formatter={(value) => [formatValue(Number(value ?? 0)), ""]}
          />
          <Area
            type="monotone"
            dataKey="value"
            stroke={color}
            fill={`url(#${gradientId})`}
            strokeWidth={1.6}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

function Kpi({
  label,
  value,
  note,
  points,
  color,
  gradientId,
  formatValue,
  fault,
}: {
  label: string;
  value: string;
  note: string;
  points: Point[];
  color: string;
  gradientId: string;
  formatValue?: (v: number) => string;
  fault?: boolean;
}) {
  return (
    <div className={`card p-4.5 pb-3.5 ${fault ? "border-fault/40 bg-fault/5" : ""}`}>
      <div className="kpi-label">{label}</div>
      <div className="kpi-value">{value}</div>
      <div className="kpi-note">{note}</div>
      <Sparkline
        points={points}
        color={fault ? "var(--color-fault)" : color}
        gradientId={gradientId}
        formatValue={formatValue}
      />
    </div>
  );
}

export default function Dashboard() {
  const [range, setRange] = useState<TimeRange>("7d");
  const [cost, setCost] = useState<Point[]>([]);
  const [tokens, setTokens] = useState<Point[]>([]);
  const [sessions, setSessions] = useState<Point[]>([]);
  const [wasted, setWasted] = useState<Point[]>([]);
  const [recent, setRecent] = useState<RecentInteraction[]>([]);
  const [insights, setInsights] = useState<Insight[]>([]);
  const [showAllFlagged, setShowAllFlagged] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getTimeseries("cost", "day", range),
      getTimeseries("tokens", "day", range),
      getTimeseries("sessions", "day", range),
    ])
      .then(([c, t, s]) => {
        setCost(c);
        setTokens(t);
        setSessions(s);
      })
      .catch((e) => setError(String(e)));
  }, [range]);

  useEffect(() => {
    getView<WastedSpend>("wasted_spend", { limit: "1000", ...(range ? { since: range } : {}) })
      .then((rows) => {
        const byDay = new Map<string, number>();
        for (const r of rows) byDay.set(r.day, (byDay.get(r.day) ?? 0) + r.wasted_cost_usd);
        setWasted(
          [...byDay.entries()]
            .map(([bucket, value]) => ({ bucket, value }))
            .sort((a, b) => a.bucket.localeCompare(b.bucket)),
        );
      })
      .catch(() => setWasted([]));
  }, [range]);

  useEffect(() => {
    listSessions(5)
      .then(async (sessionList) => {
        const [details, costRows] = await Promise.all([
          Promise.all(sessionList.map((s) => getSession(s.session_id))),
          Promise.all(
            sessionList.map((s) =>
              getView<PromptEfficiency>("prompt_efficiency", { session_id: s.session_id }),
            ),
          ),
        ]);
        const costByPrompt = new Map<string, number>();
        for (const rows of costRows) {
          for (const r of rows) costByPrompt.set(r.prompt_id, r.total_cost_usd);
        }
        const flat = details
          .flatMap((d) =>
            d.interactions.map((i) => ({
              ...i,
              session_id: d.session.session_id,
              cost_usd: costByPrompt.get(i.prompt_id) ?? 0,
            })),
          )
          .sort((a, b) => b.started_at - a.started_at)
          .slice(0, 10);
        setRecent(flat);
      })
      .catch((e) => setError(String(e)));
  }, []);

  useEffect(() => {
    getInsights(range || undefined)
      .then(setInsights)
      .catch(() => setInsights([]));
  }, [range]);

  const visibleInsights = useMemo(
    () => (showAllFlagged ? insights : insights.slice(0, 3)),
    [insights, showAllFlagged],
  );

  return (
    <PageShell
      title="Overview"
      subtitle="What was spent, where it went, and what stands out in this range."
    >
      <Toolbar
        left={<TimeRangeSelect value={range} onChange={setRange} allowAllTime={false} />}
        right={<ExportReportButton />}
      />

      {error && <p className="text-sm text-fault">{error}</p>}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Kpi
          label="Cost today"
          value={formatCostUsd(todayValue(cost))}
          note={`trailing ${timeRangeLabel(range)}`}
          points={cost}
          color="var(--color-chart)"
          gradientId="grad-cost"
          formatValue={formatCostUsd}
        />
        <Kpi
          label="Tokens today"
          value={Math.round(todayValue(tokens)).toLocaleString()}
          note={`trailing ${timeRangeLabel(range)}`}
          points={tokens}
          color="var(--color-chart)"
          gradientId="grad-tokens"
          formatValue={(v) => Math.round(v).toLocaleString()}
        />
        <Kpi
          label="Sessions today"
          value={String(Math.round(todayValue(sessions)))}
          note={`trailing ${timeRangeLabel(range)}`}
          points={sessions}
          color="var(--color-chart)"
          gradientId="grad-sessions"
          formatValue={(v) => String(Math.round(v))}
        />
        <Kpi
          label="Wasted spend"
          value={formatCostUsd(todayValue(wasted))}
          note="cost of conversations with a rejected call"
          points={wasted}
          color="var(--color-fault)"
          gradientId="grad-wasted"
          formatValue={formatCostUsd}
          fault
        />
      </div>

      <div className="card">
        <div className="card-head">
          <div className="flex items-center gap-3.5">
            <span className="card-icon">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                <path d="M4 18V8M10 18V4M16 18v-7M22 18h-20" />
              </svg>
            </span>
            <div>
              <div className="card-title">Token usage trend</div>
              <div className="card-desc">Total tokens per day, this range</div>
            </div>
          </div>
        </div>
        <div className="card-pad">
          <div className="h-56">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={tokens} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
                <defs>
                  <linearGradient id="grad-token-trend" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-ink)" stopOpacity={0.12} />
                    <stop offset="100%" stopColor="var(--color-ink)" stopOpacity={0.01} />
                  </linearGradient>
                </defs>
                <CartesianGrid vertical={false} stroke="var(--color-wire)" strokeDasharray="3 4" />
                <Tooltip
                  cursor={{ stroke: "var(--color-wire-2)", strokeWidth: 1 }}
                  contentStyle={{
                    background: "var(--color-panel-2)",
                    border: "1px solid var(--color-wire-2)",
                    borderRadius: 6,
                    fontSize: 12,
                  }}
                  labelStyle={{ color: "var(--color-ink)" }}
                  formatter={(v) => [Math.round(Number(v ?? 0)).toLocaleString(), "Tokens"]}
                />
                <Area
                  type="monotone"
                  dataKey="value"
                  stroke="var(--color-ink)"
                  fill="url(#grad-token-trend)"
                  strokeWidth={2}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      </div>

      {insights.length > 0 && (
        <div>
          <div className="mb-3 text-base font-semibold text-ink">Flagged</div>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {visibleInsights.map((i) => (
              <InsightBanner key={i.id} insight={i} />
            ))}
          </div>
          {insights.length > 3 && !showAllFlagged && (
            <button type="button" className="link-more mt-3.5" onClick={() => setShowAllFlagged(true)}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round">
                <path d="M6 9l6 6 6-6" />
              </svg>
              Show {insights.length - 3} more
            </button>
          )}
        </div>
      )}

      <div>
        <div className="mb-3 text-base font-semibold text-ink">Recent conversations</div>
        <div className="flex flex-col gap-2">
          {recent.length === 0 && (
            <div className="empty">No interactions yet.</div>
          )}
          {recent.map((i) => (
            <Link key={i.prompt_id} to={`/waterfall?session=${i.session_id}&prompt=${i.prompt_id}`} className="row">
              <span className="row-cost">{formatCostUsd(i.cost_usd)}</span>
              <span className="row-text">
                {i.command_name ?? summarizePromptText(i.prompt_text).slice(0, 120)}
              </span>
              <span className="row-when">{formatTimestamp(i.started_at)}</span>
            </Link>
          ))}
        </div>
      </div>
    </PageShell>
  );
}
