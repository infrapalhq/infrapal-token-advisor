import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useAgents } from "./agentContext";
import { SkippedChecks } from "./agents";
import { api, type Overview as OverviewData } from "./api";
import { FirstRun } from "./FirstRun";
import { Shell } from "./Shell";
import { useFirstRun } from "./status";
import { AREA_NAME, dayShort, money, pipClass, sessionsLabel, tokensHuman, waitMs } from "./format";
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

  const dayMax = useMemo(() => Math.max(1, ...(data?.by_day.map((d) => d.cost_usd) ?? [1])), [data]);
  const projMax = useMemo(() => Math.max(1, ...(data?.by_project.map((p) => p.cost_usd) ?? [1])), [data]);
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
            <h1>{noCost ? `${tokensHuman(data.tokens)} tokens used` : `${money(data.spend_usd)} spent`} {periodLabel}.</h1>
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
              <p className="eb">Spend</p>
              <div className="num">{noCost ? "—" : money(data.spend_usd)}</div>
            </div>
            <div className="kpi">
              <p className="eb">Tokens</p>
              <div className="num">{tokensHuman(data.tokens)}</div>
              <div className="note">{Math.round(data.cache_share * 100)}% served from cache</div>
            </div>
            <div className="kpi">
              <p className="eb">Avoidable</p>
              <div className="num">{noCost ? "—" : `≈ ${money(data.avoidable_usd)}`}</div>
              <div className="meter hatch" />
            </div>
            <div className="kpi">
              <p className="eb">Waiting on you</p>
              <div className="num">{waitMs(data.waiting_ms)}</div>
              <div className="note">approving commands</div>
            </div>
            <div className="kpi">
              <p className="eb">Sessions</p>
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
                  <span>
                    <strong>{it.title}</strong>
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
              {data.areas.map((a) => {
                // A card counts the problems found in sessions. Global ones (an
                // unused MCP server) belong to no session, so they are shown
                // apart, and the card always opens the sessions it counts.
                const own = a.items - a.install_wide;
                const sessions = `${a.sessions} session${a.sessions === 1 ? "" : "s"}`;
                return (
                  <Link className={`core${a.area === "context" ? " heavy" : ""}`} key={a.area} to={`/sessions?area=${a.area}`}>
                    <div className="core-top">
                      <strong>{AREA_NAME[a.area]}</strong>
                      <span className={`pip ${pipClass(a.severity === "fine" ? "" : a.severity)}`} />
                    </div>
                    <span className="num">{a.usd > 0 ? `≈ ${money(a.usd)}` : `${own} open`}</span>
                    <p>{own} issue{own === 1 ? "" : "s"} · {sessions}</p>
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
                {data.by_day.map((d) => {
                  const total = (d.cost_usd / dayMax) * 100;
                  const avoid = Math.min(total, (d.avoidable_usd / dayMax) * 100);
                  return (
                    <div className="day-col" key={d.day} style={{ height: `${Math.max(total, 2)}%` }} title={`${dayShort(Date.parse(d.day))}: ${money(d.cost_usd)}`}>
                      <div className="hatch-part hatch" style={{ height: `${avoid}%` }} />
                      <div className="solid" />
                    </div>
                  );
                })}
              </div>
              <div className="axis-x">
                {data.by_day.map((d) => (
                  <span key={d.day}>{dayShort(Date.parse(d.day))}</span>
                ))}
              </div>
            </div>

            <div className="panel proj">
              <h2>Spend by project</h2>
              {data.by_project.map((p) => (
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
                <Link to="/sessions">All sessions →</Link>
              </div>
              {data.review.map((s) => (
                <Link className="review-row" key={s.id} to={`/sessions/${encodeURIComponent(s.id)}`}>
                  <span className={`pip ${pipClass(s.areas.some((a) => a.severity === "full") ? "full" : "half")}`} />
                  <span>
                    <strong>{s.title}</strong>
                    <small>{s.project} · {dayShort(s.started_at)}</small>
                  </span>
                  <span>{s.issues} issue{s.issues === 1 ? "" : "s"}</span>
                  <span className="mono">{money(s.cost_usd)}</span>
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
