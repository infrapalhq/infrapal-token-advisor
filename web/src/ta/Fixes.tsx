import { useEffect, useState, useCallback } from "react";
import { useAgents } from "./agentContext";
import { SkippedChecks } from "./agents";
import { api, type AiConfig, type AiResult, type Fixes as FixesData, type Item, type Progress } from "./api";
import { FirstRun } from "./FirstRun";
import { Shell } from "./Shell";
import { useFirstRun } from "./status";
import { AREA_NAME, dayShort, money, sessionsLabel } from "./format";
import { useRefresh } from "./refresh";

const AI_RANGE = "30d";
const MARK_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;
// AiInsights is the only control that sends anything to a model provider. Loading the
// page reads saved results and never makes a call.
function AiInsights() {
  const [ai, setAi] = useState<AiConfig | null>(null);
  const [aiLoaded, setAiLoaded] = useState(false);
  const [result, setResult] = useState<AiResult | null>(null);
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.aiConfig().then(setAi).catch(() => {}).finally(() => setAiLoaded(true));
    api.aiRecommendations(AI_RANGE).then(setResult).catch(() => {});
  }, []);

  const generate = async (force: boolean) => {
    setGenerating(true);
    setError(null);
    try {
      setResult(await api.generateAI(AI_RANGE, force));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setGenerating(false);
    }
  };

  if (!aiLoaded) return null;
  const configured = ai?.configured ?? false;
  // Without a key the card has nothing to offer; the key is added in Settings.
  if (!configured) return null;
  const hasRun = Boolean(result?.batch_id);
  const recs = result?.recommendations ?? [];
  const dropped = result?.dropped_count ?? 0;

  return (
    <section className="card ai" aria-label="AI recommendations">
      <div className="card-top">
        <strong>AI recommendations</strong>
        <span className="ok">Key added</span>
      </div>
      <span className="body-copy">
        Optional. Generate sends the last 30 days of aggregated figures and labels to {ai?.provider_name ?? "your model provider"} with your key, never your prompts, responses, file paths or commands.
      </span>
      {recs.map((r, i) => (
        <article className="ai-rec" key={i}>
          <strong><span className={`pip ${r.severity === "warn" ? "full" : "half"}`} />{r.title}</strong>
          <span className="body-copy">{r.detail}</span>
          {r.evidence.length > 0 && (
            <span className="sub">{r.evidence.map((e) => `${e.label}: ${e.value}`).join(" · ")}</span>
          )}
        </article>
      ))}
      {hasRun && recs.length === 0 && <span className="note">Nothing to recommend yet. Try again after a few more sessions.</span>}
      {dropped > 0 && <span className="note">Left out {dropped} {dropped === 1 ? "recommendation" : "recommendations"} that didn't match your data.</span>}
      {error && <span className="note" role="alert">Could not generate: {error}</span>}
      <div className="acts">
        <button className="btn btn-k" type="button" disabled={generating} onClick={() => generate(hasRun)}>
          {generating ? "Generating…" : hasRun ? "Generate again" : "Generate"}
        </button>
      </div>
      {hasRun && <span className="note" role="status">Last generated {dayShort(result?.generated_at ?? 0)}</span>}
    </section>
  );
}

export default function Fixes() {
  const [data, setData] = useState<FixesData | null>(null);
  const [progress, setProgress] = useState<Progress | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState<string | null>(null);
  // now is when the page loaded, which is precise enough for a 7-day window.
  const [now] = useState(Date.now);
  const firstRun = useFirstRun();
  const { selected: agent } = useAgents();

  const load = useCallback(() => {
    Promise.all([api.fixes("all", agent), api.progress(agent)])
      .then(([f, p]) => {
        setData(f);
        setProgress(p);
        setError(null);
      })
      .catch((e: Error) => setError(e.message));
  }, [agent]);

  const tick = useRefresh();
  useEffect(() => {
    load();
  }, [load, firstRun, tick]);

  const mark = async (it: Item, state: "fixed" | "reverted") => {
    setBusy(it.key);
    try {
      await api.setFixState(it.key, state);
      load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const copy = (it: Item) => {
    if (!it.fix) return;
    navigator.clipboard?.writeText(it.fix).catch(() => {});
    setCopied({ ...copied, [it.key]: true });
  };

  const weeks = progress?.weeks ?? [];
  const first = weeks[0];
  const last = weeks[weeks.length - 1];
  const down = first && last && first.avoidable_usd > 0 ? Math.round((1 - last.avoidable_usd / first.avoidable_usd) * 100) : null;
  const installWide = data?.to_fix.filter((it) => it.sessions === 0).length ?? 0;
  const maxAvoid = Math.max(1, ...weeks.map((w) => w.avoidable_usd));

  if (firstRun) return <Shell><FirstRun /></Shell>;

  return (
    <Shell fixesToDo={data?.to_fix.length} sessionCount={undefined}>
      <div className="top-row">
        <p className="eb">Fixes</p>
      </div>

      <div className="fixes-head">
        <div className="title-block">
          <p className="eb">Fixes · your progress</p>
          <h1>{down !== null && down > 0 ? `Avoidable spend is down ${down}% since you started.` : "Avoidable spend since you started."}</h1>
          <p className="sub">Compared with your first week of sessions. All figures are estimated from your own sessions.</p>
        </div>
      </div>

      {error && <p className="hint-line">Could not load: {error}</p>}
      {!data && !error && <p className="hint-line">Loading…</p>}
      <SkippedChecks />

      {data && (
        <>
          <section className="progress" aria-label="Your progress">
            <div className="stats">
              <div className="stat-main">
                <p className="eb">Session health</p>
                <div className="num-line">
                  <span className="num">{last ? `${Math.round(last.healthy_pct * 100)}%` : "—"}</span>
                  <span className="num-label">healthy sessions</span>
                </div>
                {installWide > 0 && (
                  <p className="stat-note">Counts problems found inside sessions. {installWide} install-wide {installWide === 1 ? "fix" : "fixes"} below {installWide === 1 ? "isn't" : "aren't"} included.</p>
                )}
                {weeks.length > 0 && (
                  <div className="rows-bars" role="img" aria-label="Healthy sessions by week">
                    {weeks.slice(-4).map((w) => (
                      <div className="r-line" key={w.week}>
                        <span className="mono">{dayShort(Date.parse(w.week))}</span>
                        <div className="mix"><div className="ok-part" style={{ width: `${Math.round(w.healthy_pct * 100)}%` }} /></div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
              <div className="stat-grid">
                <div className="stat">
                  <p className="eb">Avoidable / week</p>
                  <span className="num">{last ? money(last.avoidable_usd) : "—"}</span>
                  <span className="was">{first ? `was ${money(first.avoidable_usd)}` : ""}</span>
                </div>
                <div className="stat">
                  <p className="eb">Open fixes</p>
                  <span className="num">{data.counts.to_fix}</span>
                  <span className="was">with a copy-paste fix</span>
                </div>
                <div className="stat">
                  <p className="eb">Habits</p>
                  <span className="num">{data.counts.habits}</span>
                  <span className="was">no config change needed</span>
                </div>
                <div className="stat">
                  <p className="eb">Marked fixed</p>
                  <span className="num">{data.counts.marked}</span>
                  <span className="was">measured for 7 days</span>
                </div>
              </div>
            </div>

            <div className="week">
              <div className="week-head">
                <h2>Week by week</h2>
                <div className="week-legend">
                  <span><span aria-hidden="true" className="hatch" style={{ width: 14, height: 12, border: "1px solid var(--k)" }} />Avoidable $ / week</span>
                  <span><span aria-hidden="true" style={{ width: 18, borderTop: "2.5px solid var(--k)" }} />Healthy sessions %</span>
                </div>
              </div>
              <div className="bars" role="img" aria-label="Avoidable spend by week">
                {weeks.map((w) => (
                  <div className="day-col" key={w.week} style={{ height: `${Math.max((w.avoidable_usd / maxAvoid) * 100, 2)}%` }} title={`${dayShort(Date.parse(w.week))}: ${money(w.avoidable_usd)} avoidable · ${Math.round(w.healthy_pct * 100)}% healthy · ${w.marked_fixes} marked`}>
                    <div className="hatch-part hatch" style={{ height: "100%" }} />
                  </div>
                ))}
                {weeks.length > 1 && (
                  <svg className="bars-line" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                    <polyline points={weeks.map((w, i) => `${((i + 0.5) / weeks.length) * 100},${100 - w.healthy_pct * 100}`).join(" ")} />
                  </svg>
                )}
              </div>
              <div className="axis-x">
                {weeks.map((w) => (
                  <span key={w.week}>{dayShort(Date.parse(w.week))}{w.marked_fixes > 0 ? ` · ${w.marked_fixes} marked` : ""}</span>
                ))}
              </div>
            </div>
          </section>

          <AiInsights />

          <div className="cols">
            <div className="col">
              <div className="colh"><strong>To fix</strong><span className="mono">{data.to_fix.length}</span></div>
              {data.to_fix.length === 0 && <p className="hint-line">Nothing with a config fix right now.</p>}
              {data.to_fix.map((it) => (
                <article className={`card${it.severity === "full" ? " heavy" : ""}`} key={it.key}>
                  <span className="chip">{AREA_NAME[it.area]}</span>
                  <strong>{it.title}</strong>
                  <span className="sub">
                    {[
                      it.usd !== undefined ? `≈ ${money(it.usd)}` : "",
                      it.minutes !== undefined ? `${Math.round(it.minutes)} min` : "",
                      sessionsLabel(it.sessions),
                      it.conf,
                    ].filter(Boolean).join(" · ")}
                  </span>
                  {it.detail && <span className="body-copy">{it.detail}</span>}
                  {it.fix && <pre className="code">{it.fix}</pre>}
                  {it.note && <span className="note">{it.note}</span>}
                  <div className="acts">
                    <button className="btn" type="button" onClick={() => copy(it)}>{copied[it.key] ? "Copied" : "Copy"}</button>
                    <button className="btn btn-k" type="button" disabled={busy === it.key} onClick={() => mark(it, "fixed")}>Mark as fixed</button>
                  </div>
                </article>
              ))}
            </div>

            <div className="col">
              <div className="colh"><strong>Marked fixed</strong><span className="mono">{data.counts.marked}</span></div>
              {data.marked.length === 0 && <p className="hint-line">Nothing marked yet. Mark a fix once you have applied it.</p>}
              {data.marked.map((it) => {
                const measuring = it.marked_at !== undefined && now - it.marked_at < MARK_WINDOW_MS;
                return (
                  <article className="card dashed" key={it.key}>
                    <div className="card-top">
                      <span className="chip">{AREA_NAME[it.area]}</span>
                      <span className="ok dashed">{measuring ? "Measuring" : "Measured"}</span>
                    </div>
                    <strong>{it.title}</strong>
                    <span className="sub-line">Marked {it.marked_at ? dayShort(it.marked_at) : ""}</span>
                    <button className="btn btn-q linkish" type="button" disabled={busy === it.key} onClick={() => mark(it, "reverted")}>
                      Not fixed after all? Move back
                    </button>
                  </article>
                );
              })}
            </div>
          </div>

          <section className="habits">
            <div className="habits-head">
              <h2>Habits — no config change needed</h2>
              <span>{data.habits.length} item{data.habits.length === 1 ? "" : "s"} fixed by how you work, not by a setting</span>
            </div>
            <div className="habit-grid">
              {data.habits.map((it) => (
                <div className="habit" key={it.key}>
                  <b>{it.title}</b>
                  <small>{it.note || it.detail}</small>
                </div>
              ))}
            </div>
          </section>
        </>
      )}
    </Shell>
  );
}
