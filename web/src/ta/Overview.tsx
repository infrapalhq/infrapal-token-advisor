import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useAgents } from "./agentContext";
import { SkippedChecks } from "./agents";
import { api, type Overview as OverviewData } from "./api";
import { FirstRun } from "./FirstRun";
import { Shell } from "./Shell";
import { useFirstRun } from "./status";
import { AREA_NAME, cleanTitle, dayShort, durationS, money, pipClass, sessionsLabel, shortPath, tokensHuman, waitMs } from "./format";
import { PERIODS, useRange } from "./range";
import { useRefresh } from "./refresh";

export default function Overview() {
  const [range, setRange] = useRange();
  // loaded remembers what its data was loaded for, so a change of range or
  // agent shows "Loading…" until the new data arrives.
  const [loaded, setLoaded] = useState<{ key: string; data: OverviewData } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const firstRun = useFirstRun();
  const { selected: agent } = useAgents();

  const key = `${range}|${agent}|${firstRun}`;
  const tick = useRefresh();
  useEffect(() => {
    api.overview(range, agent).then((d) => {
      setLoaded({ key, data: d });
      setError(null);
    }).catch((e: Error) => setError(e.message));
  }, [range, agent, key, tick]);
  const data = loaded?.key === key ? loaded.data : null;

  const days = useMemo(() => bucketDays(data?.by_day ?? []), [data]);
  const dayMax = useMemo(() => Math.max(1, ...days.map((d) => d.cost_usd)), [days]);
  const projects = useMemo(() => topProjects(data?.by_project ?? []), [data]);
  const projMax = useMemo(() => Math.max(1, ...projects.map((p) => p.cost_usd)), [projects]);
  const areas = useMemo(() => [...(data?.areas ?? [])].sort((a, b) => AREA_RANK.indexOf(a.area) - AREA_RANK.indexOf(b.area)), [data]);
  const problemCount = data?.open_fixes ?? 0;
  const recoveredUsd = (data?.recovered.usd ?? 0) >= 0.005; // would show as $0.00 otherwise
  const recoveredMinutes = Math.round(data?.recovered.minutes ?? 0);
  const unknownCost = data?.cost_unknown_sessions ?? 0;
  const noCost = unknownCost > 0 && unknownCost === data?.sessions_count;
  const periodLabel = PERIODS.find((p) => p.value === range)?.label.toLowerCase() ?? "in range";

  if (firstRun) return <Shell><FirstRun /></Shell>;

  return (
    <Shell fixesToDo={data?.open_fixes} sessionCount={data?.sessions_count}>
      <div className="top-row">
        <p className="eb">Overview</p>
        <div className="top-actions">
          <label htmlFor="period" className="sr">Period</label>
          <select id="period" className="btn" value={range} onChange={(e) => setRange(e.target.value as typeof range)}>
            {PERIODS.map((p) => (
              <option key={p.value} value={p.value}>{p.label}</option>
            ))}
          </select>
        </div>
      </div>

      {error && <p className="hint-line">Could not load: {error}</p>}
      {!data && !error && <p className="hint-line">Loading…</p>}

      {data && (
        <>
          <section className="hero">
            <h1>{noCost ? <><span className="grad">{tokensHuman(data.tokens)}</span> tokens used</> : <><span className="grad">{money(data.spend_usd)}</span> spent</>} {periodLabel}.</h1>
            <p className="lead">
              {problemCount} {problemCount === 1 ? "thing" : "things"} worth fixing
              {(recoveredUsd || recoveredMinutes > 0) ? ". Fixing the top three " : "."}
              {recoveredUsd && <>recovers about <strong>{money(data.recovered.usd)}</strong></>}
              {recoveredUsd && recoveredMinutes > 0 && " and "}
              {recoveredMinutes > 0 && <>saves about <strong>{recoveredMinutes} {recoveredMinutes === 1 ? "minute" : "minutes"}</strong> of waiting</>}
              {(recoveredUsd || recoveredMinutes > 0) && "."}
            </p>
            {unknownCost > 0 && !noCost && <p className="hint-line">Spend leaves out {unknownCost} {unknownCost === 1 ? "session" : "sessions"} with no cost data.</p>}
            <SkippedChecks />
          </section>

          <section className="kpis" aria-label="Key numbers">
            <div className="kpi">
              <p className="kl">Spend</p>
              <div className="num">{noCost ? "—" : money(data.spend_usd)}</div>
              {!noCost && data.by_day.length > 1 && <Sparkline values={data.by_day.map((d) => d.cost_usd)} />}
            </div>
            <div className="kpi">
              <p className="kl">Tokens</p>
              <div className="num">{tokensHuman(data.tokens)}</div>
              <div className="note">{Math.round(data.cache_share * 100)}% served from cache</div>
            </div>
            <div className="kpi">
              <p className="kl">Avoidable</p>
              <div className="num">{noCost ? "—" : `≈ ${money(data.avoidable_usd)}`}</div>
              <div className="meter hatch" />
            </div>
            <div className="kpi">
              <p className="kl">Waiting on you</p>
              <div className="num">{waitMs(data.waiting_ms)}</div>
              <div className="note">approving commands</div>
            </div>
            <div className="kpi">
              <p className="kl">Sessions</p>
              <div className="num">{data.sessions_count}</div>
              <div className="note">in this range</div>
            </div>
          </section>

          <section className="section">
            <div className="section-head">
              <h2 className="h2">Fix first</h2>
              <Link to="/fixes">All fixes →</Link>
            </div>
            <div className="fix-list">
              {data.fix_first.length === 0 && <p className="hint-line">Nothing to fix in this range.</p>}
              {data.fix_first.map((it) => (
                <div className="fix-row" key={it.key}>
                  <span className={`pip ${pipClass(it.severity)}`} />
                  <span className="fr-main">
                    <strong title={it.title}>{shortPath(it.title)}</strong>
                    <small>{sessionsLabel(it.sessions)} · {it.conf}</small>
                  </span>
                  <span className="chip">{AREA_NAME[it.area]}</span>
                  <span className="cost">{it.usd !== undefined ? `≈ ${money(it.usd)}` : it.minutes !== undefined ? `${Math.round(it.minutes)}m` : ""}</span>
                  <Link className="btn btn-k" to="/fixes">Fix</Link>
                </div>
              ))}
            </div>
          </section>

          <section className="section">
            <div className="section-head">
              <h2 className="h2">Four core problems</h2>
              <div className="legend">
                <span><span className="pip full" />Fix now</span>
                <span><span className="pip half" />Worth a look</span>
                <span><span className="pip" />Fine</span>
              </div>
            </div>
            <div className="cores">
              {areas.map((a) => {
                // A card counts the problems found in sessions. Global ones (an
                // unused MCP server) belong to no session, so they are shown
                // apart, and the card always opens the sessions it counts.
                const own = a.items - a.install_wide;
                const sessions = `${a.sessions} session${a.sessions === 1 ? "" : "s"}`;
                return (
                  <Link className={`core area-${a.area}${a.severity !== "fine" && a.items > 0 ? " lit" : ""}`} key={a.area} to={`/sessions?area=${a.area}`}>
                    <div className="core-top">
                      <strong>{AREA_NAME[a.area]}</strong>
                      <span className={`pip ${pipClass(a.severity === "fine" ? "" : a.severity)}`} />
                    </div>
                    <span className="num">{a.usd > 0 ? `≈ ${money(a.usd)}` : `${own} open`}</span>
                    <p>{own} issue{own === 1 ? "" : "s"} · {AREA_BLURB[a.area] ?? sessions}</p>
                    {a.install_wide > 0 && <p className="core-global">+ {a.install_wide} global {a.install_wide === 1 ? "fix" : "fixes"}, under Fixes</p>}
                    {a.items > 0 && <span className="go">{sessions} →</span>}
                  </Link>
                );
              })}
            </div>
          </section>

          {!noCost && <section className="charts">
            <div className="panel day">
              <div className="panel-head">
                <h2>Spend by day</h2>
                <span className="panel-note">hatched = avoidable</span>
              </div>
              <div className="bars" role="img" aria-label="Daily spend">
                {days.map((d) => {
                  const total = (d.cost_usd / dayMax) * 100;
                  const avoid = Math.min(total, (d.avoidable_usd / dayMax) * 100);
                  return (
                    <div className="day-col" key={d.day} style={{ height: `${Math.max(total, 2)}%` }} title={`${d.label}: ${money(d.cost_usd)}`}>
                      <div className="hatch-part hatch" style={{ height: `${avoid}%` }} />
                      <div className="solid" />
                    </div>
                  );
                })}
              </div>
              <div className="axis-x">
                {days.map((d, i) => (
                  <span key={d.day}>{i % labelEvery(days.length) === 0 ? d.label : ""}</span>
                ))}
              </div>
            </div>

            <div className="panel proj">
              <h2>Spend by project</h2>
              {projects.map((p) => (
                <div className="proj-row" key={p.project}>
                  <div className="top">
                    <span className="mono">{p.project}</span>
                    <span className="mono">{money(p.cost_usd)}</span>
                  </div>
                  <div className="track">
                    <div className="fill" style={{ width: `${(p.cost_usd / projMax) * 100}%` }} />
                  </div>
                </div>
              ))}
              {data.models.some((m) => m.share >= 0.005) && (
                <span className="foot">
                  Models: {data.models.filter((m) => m.share >= 0.005).slice(0, 3).map((m) => `${m.model} ${Math.round(m.share * 100)}%`).join(" · ")}
                </span>
              )}
            </div>
          </section>}

          {data.review.length > 0 && (
            <section className="section">
              <div className="section-head">
                <h2 className="h2">Sessions to review</h2>
                <Link to="/sessions">All {data.sessions_count} sessions →</Link>
              </div>
              {data.review.map((s) => (
                <Link className="review-row" key={s.id} to={`/sessions/${encodeURIComponent(s.id)}`}>
                  <span className={`pip ${pipClass(s.areas.some((a) => a.severity === "full") ? "full" : "half")}`} />
                  <span>
                    <strong className="q">{cleanTitle(s.title)}</strong>
                    <small>{s.project} · {dayShort(s.started_at)} · {durationS(s.duration_s)}</small>
                  </span>
                  <span className="iss">{s.issues} issue{s.issues === 1 ? "" : "s"}</span>
                  <span className="mono cost">{money(s.cost_usd)}</span>
                  <span className="go">Open →</span>
                </Link>
              ))}
            </section>
          )}
        </>
      )}
    </Shell>
  );
}

// The core-problem cards always run in this order.
const AREA_RANK = ["model", "context", "exec", "perm"];
const AREA_BLURB: Record<string, string> = {
  model: "a bigger model than the job needs",
  context: "re-reads, compactions, mixed tasks",
  exec: "repeat runs and failing tools",
  perm: "repeat approvals and rejections",
};

type DayBar = { day: string; label: string; cost_usd: number; avoidable_usd: number };

// bucketDays keeps the chart readable: up to two weeks shows a bar per day,
// anything longer a bar per week, labelled by the week's first day.
function bucketDays(days: OverviewData["by_day"]): DayBar[] {
  if (days.length <= 14) {
    return days.map((d) => ({ ...d, label: dayShort(Date.parse(d.day)) }));
  }
  const out: DayBar[] = [];
  for (let i = 0; i < days.length; i += 7) {
    const wk = days.slice(i, i + 7);
    out.push({
      day: wk[0].day,
      label: dayShort(Date.parse(wk[0].day)),
      cost_usd: wk.reduce((n, d) => n + d.cost_usd, 0),
      avoidable_usd: wk.reduce((n, d) => n + d.avoidable_usd, 0),
    });
  }
  return out;
}

// labelEvery thins the x-axis so labels never run into each other.
function labelEvery(n: number): number {
  return n <= 8 ? 1 : Math.ceil(n / 7);
}

const TOP_PROJECTS = 5;

// topProjects shows the biggest projects and folds the rest into "Other".
function topProjects(list: OverviewData["by_project"]): OverviewData["by_project"] {
  if (list.length <= TOP_PROJECTS + 1) return list;
  const head = list.slice(0, TOP_PROJECTS);
  const rest = list.slice(TOP_PROJECTS);
  return [...head, {
    project: `Other (${rest.length} projects)`,
    cost_usd: rest.reduce((n, p) => n + p.cost_usd, 0),
    sessions: rest.reduce((n, p) => n + p.sessions, 0),
  }];
}

function Sparkline({ values }: { values: number[] }) {
  const max = Math.max(...values, 0.0001);
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * 100},${28 - (v / max) * 24}`).join(" ");
  return (
    <svg className="spark" viewBox="0 0 100 30" preserveAspectRatio="none" aria-hidden="true">
      <polyline points={pts} />
    </svg>
  );
}
