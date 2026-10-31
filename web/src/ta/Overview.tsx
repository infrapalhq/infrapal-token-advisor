import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, type Overview as OverviewData, type Range } from "./api";
import { Shell } from "./Shell";
import { AREA_NAME, areaCounts, areaLink, dayShort, itemSessions, money, pipClass, tokensHuman, waitMs } from "./format";

const PERIODS: { value: Range; label: string }[] = [
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "today", label: "Today" },
];

export default function Overview() {
  const [params, setParams] = useSearchParams();
  const range = (params.get("range") as Range) || "7d";
  const [data, setData] = useState<OverviewData | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setData(null);
    api.overview(range).then(setData).catch((e: Error) => setError(e.message));
  }, [range]);

  const dayMax = useMemo(() => Math.max(1, ...(data?.by_day.map((d) => d.cost_usd) ?? [1])), [data]);
  const projMax = useMemo(() => Math.max(1, ...(data?.by_project.map((p) => p.cost_usd) ?? [1])), [data]);
  const problemCount = data?.fix_first.length ?? 0;

  return (
    <Shell fixesToDo={data?.fix_first.length} sessionCount={data?.sessions_count}>
      <div className="top-row">
        <p className="eb">Overview</p>
        <div className="top-actions">
          <label htmlFor="period" className="sr">Period</label>
          <select id="period" className="btn" value={range} onChange={(e) => setParams({ range: e.target.value })}>
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
            <h1>{money(data.spend_usd)} spent {PERIODS.find((p) => p.value === range)?.label.toLowerCase() ?? "in range"}.</h1>
            <p className="lead">
              {problemCount} {problemCount === 1 ? "thing" : "things"} worth fixing
              {data.fix_first.length > 0 && <>. Fixing the top three recovers about <strong>{money(data.recovered.usd)}</strong>
                {data.recovered.minutes > 0 && <> and <strong>{Math.round(data.recovered.minutes)} minutes</strong></>} of waiting or spend.</>}
            </p>
          </section>

          <section className="kpis" aria-label="Key numbers">
            <div className="kpi">
              <p className="eb">Spend</p>
              <div className="num">{money(data.spend_usd)}</div>
            </div>
            <div className="kpi">
              <p className="eb">Tokens</p>
              <div className="num">{tokensHuman(data.tokens)}</div>
              <div className="note">{Math.round(data.cache_share * 100)}% served from cache</div>
            </div>
            <div className="kpi">
              <p className="eb">Avoidable</p>
              <div className="num">≈ {money(data.avoidable_usd)}</div>
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
                    <small>{itemSessions(it.sessions, it.scope)} · {it.conf}{it.spent_usd ? ` · ${money(it.spent_usd)} ${it.spent_label ?? ""}` : ""}</small>
                  </span>
                  <span className="chip">{AREA_NAME[it.area]}</span>
                  <span className="cost">{it.usd !== undefined ? `≈ ${money(it.usd)}` : it.minutes !== undefined ? `${Math.round(it.minutes)}m` : ""}</span>
                  <Link className="btn btn-k" to="/fixes">{it.fix ? "Fix" : "View"}</Link>
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
              {data.areas.map((a) => (
                <Link className={`core${a.area === "context" ? " heavy" : ""}`} key={a.area} to={a.sessions === 0 && a.setup_items > 0 ? "/fixes" : "/sessions"}>
                  <div className="core-top">
                    <strong>{AREA_NAME[a.area]}</strong>
                    <span className={`pip ${pipClass(a.severity === "fine" ? "" : a.severity)}`} />
                  </div>
                  <span className="num">{a.usd > 0 ? `≈ ${money(a.usd)}` : `${a.items} open`}</span>
                  <p>{areaCounts(a.items - a.setup_items, a.sessions, a.setup_items)}</p>
                  <span className="go">{areaLink(a.sessions, a.setup_items)} →</span>
                </Link>
              ))}
            </div>
          </section>

          <section className="charts">
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
              <span className="foot">
                Models: {data.models.slice(0, 3).map((m) => `${m.model} ${Math.round(m.share * 100)}%`).join(" · ")}
              </span>
            </div>
          </section>

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
