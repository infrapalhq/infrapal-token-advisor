import { useEffect, useState } from "react";
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
import { timeRangeLabel, type TimeRange } from "../lib/timeRange";
import {
  listSessions,
  getSession,
  getTimeseries,
  getInsights,
  type Insight,
  type SessionInteraction,
} from "../lib/api";
import {
  formatCostUsd,
  formatDurationMs,
  formatTimestamp,
  summarizePromptText,
} from "../lib/format";

type Point = { bucket: string; value: number };
type RecentInteraction = SessionInteraction & { session_id: string };

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
  if (points.length < 2) return <div className="h-14" />;
  return (
    <div className="h-14">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={points} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.35} />
              <stop offset="100%" stopColor={color} stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid
            vertical={false}
            stroke="var(--color-wire)"
            strokeDasharray="3 3"
          />
          <Tooltip
            cursor={{ stroke: "var(--color-wire)", strokeWidth: 1 }}
            contentStyle={{
              background: "var(--color-panel)",
              border: "1px solid var(--color-wire)",
              borderRadius: 4,
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
            strokeWidth={1.75}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

function StatCard({
  label,
  value,
  points,
  color,
  gradientId,
  formatValue,
  rangeLabel,
}: {
  label: string;
  value: string;
  points: Point[];
  color: string;
  gradientId: string;
  formatValue?: (v: number) => string;
  rangeLabel: string;
}) {
  return (
    <div className="rounded border border-wire bg-panel p-4">
      <div className="text-[11px] uppercase tracking-wide text-ink/50">{label}</div>
      <div className="mt-1 font-mono text-2xl text-ink">{value}</div>
      <Sparkline
        points={points}
        color={color}
        gradientId={gradientId}
        formatValue={formatValue}
      />
      <div className="text-[10px] text-ink/40">trailing {rangeLabel}</div>
    </div>
  );
}

export default function Dashboard() {
  const [range, setRange] = useState<TimeRange>("7d");
  const [cost, setCost] = useState<Point[]>([]);
  const [tokens, setTokens] = useState<Point[]>([]);
  const [sessions, setSessions] = useState<Point[]>([]);
  const [activeUser, setActiveUser] = useState<Point[]>([]);
  const [recent, setRecent] = useState<RecentInteraction[]>([]);
  const [insights, setInsights] = useState<Insight[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getTimeseries("cost", "day", range),
      getTimeseries("tokens", "day", range),
      getTimeseries("sessions", "day", range),
      getTimeseries("active_time_user", "day", range),
    ])
      .then(([c, t, s, a]) => {
        setCost(c);
        setTokens(t);
        setSessions(s);
        setActiveUser(a);
      })
      .catch((e) => setError(String(e)));
  }, [range]);

  useEffect(() => {
    listSessions(5)
      .then(async (sessionList) => {
        const details = await Promise.all(
          sessionList.map((s) => getSession(s.session_id)),
        );
        const flat = details
          .flatMap((d) =>
            d.interactions.map((i) => ({
              ...i,
              session_id: d.session.session_id,
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

  return (
    <PageShell
      title="Dashboard"
      subtitle={`Flagged insights, cost, tokens, sessions, and active time — trailing ${timeRangeLabel(range)}`}
    >
      <div className="mb-4 flex items-center justify-end">
        <TimeRangeSelect value={range} onChange={setRange} allowAllTime={false} />
      </div>

      {error && <p className="mb-4 text-sm text-fault">{error}</p>}

      {insights.map((i) => (
        <InsightBanner key={i.id} insight={i} />
      ))}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard
          label="Cost today (approx.)"
          value={formatCostUsd(todayValue(cost))}
          points={cost}
          color="#d97706"
          gradientId="grad-cost"
          formatValue={formatCostUsd}
          rangeLabel={timeRangeLabel(range)}
        />
        <StatCard
          label="Tokens today"
          value={Math.round(todayValue(tokens)).toLocaleString()}
          points={tokens}
          color="#7c3aed"
          gradientId="grad-tokens"
          formatValue={(v) => Math.round(v).toLocaleString()}
          rangeLabel={timeRangeLabel(range)}
        />
        <StatCard
          label="Sessions today"
          value={String(Math.round(todayValue(sessions)))}
          points={sessions}
          color="#0284c7"
          gradientId="grad-sessions"
          formatValue={(v) => String(Math.round(v))}
          rangeLabel={timeRangeLabel(range)}
        />
        <StatCard
          label="Active time today"
          value={formatDurationMs(todayValue(activeUser) * 1000)}
          points={activeUser}
          color="#059669"
          gradientId="grad-active-time"
          formatValue={(v) => formatDurationMs(v * 1000)}
          rangeLabel={timeRangeLabel(range)}
        />
      </div>

      <h2 className="mb-2 mt-8 font-display text-sm font-semibold text-ink/70">
        Recent interactions
      </h2>
      <div className="overflow-hidden rounded border border-wire">
        {recent.length === 0 && (
          <div className="p-4 text-sm text-ink/50">No interactions yet.</div>
        )}
        {recent.map((i) => (
          <Link
            key={i.prompt_id}
            to={`/waterfall?session=${i.session_id}&prompt=${i.prompt_id}`}
            className="flex items-center justify-between border-b border-wire/40 px-4 py-2 text-sm last:border-b-0 hover:bg-wire/20"
          >
            <span className="truncate text-ink">
              {i.command_name ?? summarizePromptText(i.prompt_text).slice(0, 80)}
            </span>
            <span className="ml-4 shrink-0 font-mono text-xs text-ink/50">
              {formatTimestamp(i.started_at)}
            </span>
          </Link>
        ))}
      </div>
    </PageShell>
  );
}
