import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { PageShell } from "../components/PageShell";
import { InsightBanner } from "../components/InsightBanner";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { Toolbar, ExportReportButton } from "../components/Toolbar";
import { type TimeRange } from "../lib/timeRange";
import {
  getView,
  getInsights,
  type Insight,
  type WastedSpend,
  type PermissionFriction,
  type PromptEfficiency,
} from "../lib/api";
import { formatCostUsd, formatDurationMs } from "../lib/format";

function toolLabel(row: PermissionFriction): string {
  if (row.mcp_server_name) {
    return `${row.mcp_server_name} · ${row.mcp_tool_name ?? "?"}`;
  }
  return row.tool_name;
}

export default function Waste() {
  const navigate = useNavigate();
  const [range, setRange] = useState<TimeRange>("");
  const [wasted, setWasted] = useState<WastedSpend[]>([]);
  const [friction, setFriction] = useState<PermissionFriction[]>([]);
  const [pressure, setPressure] = useState<PromptEfficiency[]>([]);
  const [insights, setInsights] = useState<Insight[]>([]);
  const [dayFilter, setDayFilter] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getView<WastedSpend>("wasted_spend", { limit: "1000", ...(range ? { since: range } : {}) }),
      getView<PermissionFriction>("permission_friction", { limit: "200" }),
      getView<PromptEfficiency>("prompt_efficiency", {
        limit: "1000",
        ...(range ? { since: range } : {}),
      }),
    ])
      .then(([w, f, p]) => {
        setWasted([...w].sort((a, b) => b.wasted_cost_usd - a.wasted_cost_usd));
        setFriction(
          [...f]
            .filter((r) => r.user_temporary_count > 0)
            .sort((a, b) => b.user_temporary_count - a.user_temporary_count),
        );
        setPressure(p.filter((r) => r.triggered_compaction || r.hit_max_tokens));
      })
      .catch((e) => setError(String(e)));
  }, [range]);

  useEffect(() => {
    getInsights()
      .then((all) => setInsights(all.filter((i) => i.category === "waste")))
      .catch(() => setInsights([]));
  }, []);

  const filteredWasted = useMemo(
    () => (dayFilter ? wasted.filter((r) => r.day === dayFilter) : wasted),
    [wasted, dayFilter],
  );

  const totalWasted = useMemo(
    () => wasted.reduce((sum, r) => sum + r.wasted_cost_usd, 0),
    [wasted],
  );
  const totalRejected = useMemo(
    () => wasted.reduce((sum, r) => sum + r.rejected_tool_count, 0),
    [wasted],
  );
  const totalBlockedMs = useMemo(
    () => friction.reduce((sum, r) => sum + r.total_blocked_on_user_ms, 0),
    [friction],
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

  const wasteScaleMax = Math.ceil(Math.max(1, ...dailyWaste.map((d) => d.wasted_cost_usd)) / 3) * 3 || 3;

  const wasteBySession = useMemo(() => {
    const bySession = new Map<string, { day: string; rejected: number; cost: number }>();
    for (const r of wasted) {
      const existing = bySession.get(r.session_id);
      if (existing) {
        existing.rejected += r.rejected_tool_count;
        existing.cost += r.wasted_cost_usd;
        if (r.day > existing.day) existing.day = r.day;
      } else {
        bySession.set(r.session_id, { day: r.day, rejected: r.rejected_tool_count, cost: r.wasted_cost_usd });
      }
    }
    return [...bySession.entries()]
      .map(([session_id, v]) => ({ session_id, ...v }))
      .sort((a, b) => b.cost - a.cost);
  }, [wasted]);

  return (
    <PageShell title="Waste" subtitle="Spend on work that was rejected, blocked or rebuilt.">
      <Toolbar
        left={<TimeRangeSelect value={range} onChange={setRange} />}
        right={<ExportReportButton />}
      />

      {error && <p className="text-sm text-fault">{error}</p>}

      {insights.map((i) => (
        <InsightBanner key={i.id} insight={i} />
      ))}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div className="card p-4.5">
          <div className="kpi-label">Wasted spend</div>
          <div className="kpi-value text-fault">{formatCostUsd(totalWasted)}</div>
          <div className="kpi-note">cost of conversations containing a rejected call</div>
        </div>
        <div className="card p-4.5">
          <div className="kpi-label">Rejected tool calls</div>
          <div className="kpi-value">{totalRejected}</div>
          <div className="kpi-note">across {wasted.length} session/day rows</div>
        </div>
        <div className="card p-4.5">
          <div className="kpi-label">Blocked on you</div>
          <div className="kpi-value">{formatDurationMs(totalBlockedMs)}</div>
          <div className="kpi-note">waiting on permission decisions</div>
        </div>
        <div className="card p-4.5">
          <div className="kpi-label">Context pressure</div>
          <div className="kpi-value">{pressure.length}</div>
          <div className="kpi-note">conversations that compacted or hit max tokens</div>
        </div>
      </div>
      <p className="text-[12.5px] text-ink/35">
        Wasted spend attributes a conversation's full cost when any of its tool calls was
        rejected. Tool calls carry no individual cost, so finer attribution is not possible.
      </p>

      {dailyWaste.length > 0 && (
        <div className="card card-pad">
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="card-title">Wasted spend</div>
              <div className="card-desc">
                {dayFilter ? `Filtered to ${dayFilter} — click again to clear` : "Click a bar to filter the tables below"}
              </div>
            </div>
          </div>
          <div className="mt-5 flex gap-3">
            <div className="flex h-[180px] flex-col justify-between text-right text-[11px] text-ink/35">
              <span>{wasteScaleMax}</span>
              <span>{Math.round((wasteScaleMax / 3) * 2)}</span>
              <span>{Math.round(wasteScaleMax / 3)}</span>
              <span>0</span>
            </div>
            <div className="min-w-0 flex-1">
              <div
                className="grid h-[180px] items-end gap-6 border-b border-wire"
                style={{ gridTemplateColumns: `repeat(${dailyWaste.length}, minmax(0,1fr))` }}
              >
                {dailyWaste.map((d) => (
                  <button
                    key={d.day}
                    type="button"
                    onClick={() => setDayFilter((cur) => (cur === d.day ? null : d.day))}
                    className={`rounded-t border border-b-0 bg-fault/10 hover:bg-fault/20 ${dayFilter === d.day ? "border-fault" : "border-fault/50"}`}
                    style={{ height: `${Math.max(2, (d.wasted_cost_usd / wasteScaleMax) * 100)}%` }}
                    title={`${d.day}: ${formatCostUsd(d.wasted_cost_usd)}`}
                  />
                ))}
              </div>
              <div
                className="mt-2 grid gap-6 text-center text-[11px] text-ink/35"
                style={{ gridTemplateColumns: `repeat(${dailyWaste.length}, minmax(0,1fr))` }}
              >
                {dailyWaste.map((d) => (
                  <span key={d.day}>{d.day}</span>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}

      <div className="card">
        <div className="card-head">
          <div className="flex items-center gap-3.5">
            <span className="card-icon">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
              </svg>
            </span>
            <div>
              <div className="card-title">Permission friction</div>
              <div className="card-desc">Time the agent spent waiting on a decision, by tool identity</div>
            </div>
          </div>
        </div>
        <div className="overflow-x-auto">
          <table>
            <thead>
              <tr>
                <th>Tool</th>
                <th className="num">Total calls</th>
                <th className="num">Temporary allows</th>
                <th className="num">Permanent allows</th>
                <th className="num">Time blocked</th>
              </tr>
            </thead>
            <tbody>
              {friction.map((r) => (
                <tr key={`${r.tool_name}-${r.mcp_server_name}-${r.mcp_tool_name}`}>
                  <td><span className="code">{toolLabel(r)}</span></td>
                  <td className="num">{r.total_calls}</td>
                  <td className="num">{r.user_temporary_count}</td>
                  <td className="num dim">{r.user_permanent_count}</td>
                  <td className="num fault">{formatDurationMs(r.total_blocked_on_user_ms)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {friction.length === 0 && (
            <div className="empty">No repeated permission prompts — nothing to promote to an allow-rule.</div>
          )}
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <div className="flex items-center gap-3.5">
            <span className="card-icon">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </span>
            <div>
              <div className="card-title">Rejected calls</div>
              <div className="card-desc">What the agent attempted, and where the tokens went</div>
            </div>
          </div>
        </div>
        <div className="overflow-x-auto">
          <table className="min-w-[600px]">
            <thead>
              <tr>
                <th>Session</th>
                <th>Day</th>
                <th className="num">Wasted cost</th>
                <th className="num">Rejected calls</th>
              </tr>
            </thead>
            <tbody>
              {filteredWasted.map((r) => (
                <tr
                  key={`${r.session_id}-${r.day}`}
                  className="row-link"
                  onClick={() => navigate(`/waterfall?session=${r.session_id}`)}
                >
                  <td><span className="code">{r.session_id.slice(0, 8)}</span></td>
                  <td className="dim">{r.day}</td>
                  <td className="num fault">{formatCostUsd(r.wasted_cost_usd)}</td>
                  <td className="num">{r.rejected_tool_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {filteredWasted.length === 0 && (
            <div className="empty">No rejected tool calls recorded — nothing wasted yet.</div>
          )}
        </div>
      </div>

      {pressure.length > 0 && (
        <div className="card">
          <div className="card-head">
            <div className="flex items-center gap-3.5">
              <span className="card-icon">
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                  <path d="M4 6h16M4 12h10M4 18h6" />
                </svg>
              </span>
              <div>
                <div className="card-title">Context pressure</div>
                <div className="card-desc">Conversations that compacted or hit the output ceiling</div>
              </div>
            </div>
          </div>
          <div className="overflow-x-auto">
            <table>
              <thead>
                <tr>
                  <th>Conversation</th>
                  <th className="num">Cost</th>
                  <th className="num">Tokens</th>
                  <th className="num">Turns</th>
                  <th>Condition</th>
                </tr>
              </thead>
              <tbody>
                {pressure.map((p) => (
                  <tr
                    key={p.prompt_id}
                    className="row-link"
                    onClick={() => navigate(`/waterfall?session=${p.session_id}&prompt=${p.prompt_id}`)}
                  >
                    <td><span className="code">{p.prompt_id.slice(0, 8)}</span></td>
                    <td className="num strong">{formatCostUsd(p.total_cost_usd)}</td>
                    <td className="num">{p.total_tokens.toLocaleString()}</td>
                    <td className="num">{p.llm_turn_count}</td>
                    <td className="dim">
                      {[p.triggered_compaction ? "Compaction" : null, p.hit_max_tokens ? "Max tokens" : null]
                        .filter(Boolean)
                        .join(", ")}
                    </td>
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
                <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
              </svg>
            </span>
            <div>
              <div className="card-title">Waste by session</div>
              <div className="card-desc">Routes into the session detail</div>
            </div>
          </div>
        </div>
        <div className="overflow-x-auto">
          <table>
            <thead>
              <tr>
                <th>Session</th>
                <th>Day</th>
                <th className="num">Rejected calls</th>
                <th className="num">Wasted spend</th>
              </tr>
            </thead>
            <tbody>
              {wasteBySession.map((w) => (
                <tr
                  key={w.session_id}
                  className="row-link"
                  onClick={() => navigate(`/waterfall?session=${w.session_id}`)}
                >
                  <td><span className="code">{w.session_id.slice(0, 8)}</span></td>
                  <td className="dim">{w.day}</td>
                  <td className="num">{w.rejected}</td>
                  <td className="num strong fault">{formatCostUsd(w.cost)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </PageShell>
  );
}
