import { useEffect, useState, useCallback } from "react";
import { api, type Fixes as FixesData, type Item, type McpSetup, type Progress } from "./api";
import { Shell } from "./Shell";
import { AREA_NAME, dayShort, money } from "./format";

const MARK_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;
const MCP_SAMPLE_PROMPT = "Use infrapal-token-advisor to find my three biggest sources of avoidable Claude Code spend in the last 30 days, and tell me what to change first.";

function CopyBlock({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="mcp-copy">
      <pre className="code">{text}</pre>
      <button
        className="btn"
        type="button"
        onClick={() => {
          navigator.clipboard?.writeText(text).catch(() => {});
          setCopied(true);
        }}
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}

function EnableMcp() {
  const [setup, setSetup] = useState<McpSetup | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);

  const check = useCallback(() => {
    setChecking(true);
    api.mcpSetup()
      .then((s) => {
        setSetup(s);
        setError(null);
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setChecking(false));
  }, []);

  useEffect(() => {
    check();
  }, [check]);

  if (error && !setup) return <p className="hint-line">Could not load MCP setup: {error}</p>;
  if (!setup) return null;

  return (
    <section className="card mcp" aria-label="Enable MCP">
      <div className="card-top">
        <strong>{setup.enabled ? "MCP is enabled" : "Enable MCP to ask Claude about this data"}</strong>
        <span className={`ok${setup.enabled ? "" : " dashed"}`}>{setup.enabled ? "On" : "Off"}</span>
      </div>
      <span className="body-copy">
        Claude Code can query your spend, tokens, sessions and findings and work out insights this page doesn't show.
        When it does, those results go to Anthropic with the conversation. It gets numbers and ids only, not your prompts, responses, file paths or commands.
      </span>
      {!setup.enabled && (
        <>
          <div className="mcp-step">
            <b>1. Paste this in your terminal</b>
            <CopyBlock text={setup.command} />
          </div>
          <div className="mcp-step">
            <b>2. Check that it shows as connected</b>
            <CopyBlock text={setup.verify} />
          </div>
        </>
      )}
      <div className="mcp-step">
        <b>{setup.enabled ? "Start a new Claude Code session and ask" : "3. Start a new Claude Code session and ask"}</b>
        <CopyBlock text={MCP_SAMPLE_PROMPT} />
      </div>
      {setup.enabled ? (
        <div className="mcp-step">
          <b>To turn it off</b>
          <CopyBlock text={setup.remove} />
        </div>
      ) : (
        <div className="acts">
          <button className="btn btn-k" type="button" disabled={checking} onClick={check}>{checking ? "Checking…" : "Check again"}</button>
        </div>
      )}
    </section>
  );
}

export default function Fixes() {
  const [data, setData] = useState<FixesData | null>(null);
  const [progress, setProgress] = useState<Progress | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(() => {
    Promise.all([api.fixes("all"), api.progress()])
      .then(([f, p]) => {
        setData(f);
        setProgress(p);
      })
      .catch((e: Error) => setError(e.message));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

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
  const maxAvoid = Math.max(1, ...weeks.map((w) => w.avoidable_usd));

  return (
    <Shell fixesToDo={data?.to_fix.length} sessionCount={undefined}>
      <EnableMcp />

      <div className="fixes-head">
        <div className="title-block">
          <p className="eb">Fixes · your progress</p>
          <h1>{down !== null && down > 0 ? `Avoidable spend is down ${down}% since you started.` : "Avoidable spend since you started."}</h1>
          <p className="sub">Compared with your first week of sessions. All figures are estimated from your own sessions.</p>
        </div>
      </div>

      {error && <p className="hint-line">Could not load: {error}</p>}
      {!data && !error && <p className="hint-line">Loading…</p>}

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
                  <span><span aria-hidden="true" className="hatch" style={{ width: 14, height: 12, border: "1px solid #000" }} />Avoidable $ / week</span>
                  <span><span aria-hidden="true" style={{ width: 18, borderTop: "2.5px solid #000" }} />Healthy sessions %</span>
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

          <div className="cols">
            <div className="col">
              <div className="colh"><strong>To fix</strong><span className="mono">{data.to_fix.length}</span></div>
              {data.to_fix.length === 0 && <p className="hint-line">Nothing with a config fix right now.</p>}
              {data.to_fix.map((it) => (
                <article className={`card${it.severity === "full" ? " heavy" : ""}`} key={it.key}>
                  <span className="chip">{AREA_NAME[it.area]}</span>
                  <strong>{it.title}</strong>
                  <span className="sub">
                    {it.usd !== undefined ? `≈ ${money(it.usd)}` : ""}{it.minutes !== undefined ? ` · ${Math.round(it.minutes)} min` : ""} · {it.sessions} session{it.sessions === 1 ? "" : "s"} · {it.conf}
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
                const measuring = it.marked_at !== undefined && Date.now() - it.marked_at < MARK_WINDOW_MS;
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
