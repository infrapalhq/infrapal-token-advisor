import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, type Occurrence, type SessionCard, type Timeline, type TimelineRow } from "./api";
import { AREA_LABEL, AREA_NAME, dayShort, durationS, money, tokensHuman } from "./format";

type Mode = "issues" | "receipt";
type AreaKey = "context" | "exec" | "perm" | "model";
const AREA_ORDER: AreaKey[] = ["context", "exec", "perm", "model"];

export default function SessionPage() {
  const { id = "" } = useParams();
  const [timeline, setTimeline] = useState<Timeline | null>(null);
  const [issues, setIssues] = useState<Record<string, Occurrence[]>>({});
  const [cards, setCards] = useState<SessionCard[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<Mode>("issues");
  const [area, setArea] = useState<AreaKey>("context");
  const [highlight, setHighlight] = useState<string | null>(null);
  const [marked, setMarked] = useState<Record<string, boolean>>({});
  const [copied, setCopied] = useState<Record<string, boolean>>({});

  useEffect(() => {
    setTimeline(null);
    setError(null);
    Promise.all([api.timeline(id), api.issues(id), api.sessionCards("all")])
      .then(([t, i, c]) => {
        setTimeline(t);
        setIssues(i.areas);
        setCards(c.sessions);
        const first = AREA_ORDER.find((a) => (i.areas[a] ?? []).length > 0);
        if (first) setArea(first);
      })
      .catch((e: Error) => setError(e.message));
  }, [id]);

  const current = useMemo(() => cards.find((c) => c.id === id), [cards, id]);
  const issueCount = AREA_ORDER.reduce((n, a) => n + (issues[a]?.length ?? 0), 0);
  const flagged = useMemo(() => {
    const m = new Map<string, Occurrence>();
    for (const list of Object.values(issues)) {
      for (const o of list) for (const ev of o.Evidence) if (!m.has(ev)) m.set(ev, o);
    }
    return m;
  }, [issues]);

  if (error) return <div className="shell"><p className="hint-line">Could not load: {error}</p></div>;
  if (!timeline) return <div className="shell"><p className="hint-line">Loading…</p></div>;

  const peak = Math.max(1, timeline.peak_context);
  const ctxRows = timeline.rows.filter((r) => typeof r.context_tokens === "number");
  const bars = groupBars(ctxRows, flagged, MAX_BARS);
  const jump = (rowId: string) => {
    setHighlight(rowId);
    document.getElementById(`row-${rowId}`)?.scrollIntoView({ block: "center", behavior: "smooth" });
  };
  const fixKey = (o: Occurrence) => `${o.Kind}|${o.Subject}`;

  return (
    <div className="shell">
      <aside className="side side-compact" aria-label="Sessions">
        <div className="brand brand-sm">
          <div className="brand-mark" aria-hidden="true" />
          <span>infrapal</span>
        </div>
        <nav className="side-nav" aria-label="Main">
          <Link to="/">Overview</Link>
          <Link className="on" to="/sessions" aria-current="page">Sessions</Link>
          <Link to="/fixes">Fixes</Link>
        </nav>
        <div className="sl-group">
          {cards.map((c) => (
            <Link key={c.id} className={`sl${c.id === id ? " on" : ""}${c.priority === "fine" ? " quiet" : ""}`} to={`/sessions/${encodeURIComponent(c.id)}`}>
              <span className={`pip ${c.priority === "now" ? "wfull" : c.priority === "look" ? "whalf" : "woff"}`} />
              <span className="name">{c.title}</span>
              <span className="cost">{money(c.cost_usd)}</span>
            </Link>
          ))}
        </div>
      </aside>

      <section className="thread" aria-label="Session thread">
        <header className="thread-head">
          <div className="thread-title-block">
            <p className="eb"><Link className="crumb" to="/sessions">← Sessions</Link> · {current?.project ?? ""}</p>
            <h1 className="thread-title">{current?.title ?? id}</h1>
            <div className="meta-row">
              <span className="pill">{dayShort(timeline.started_at)}</span>
              <span className="pill">{durationS(current?.duration_s ?? 0)}</span>
              <span className="pill strong">{money(timeline.receipt.cost_usd)}</span>
              <span className="pill">{tokensHuman(timeline.receipt.tokens.fresh_input + timeline.receipt.tokens.output + timeline.receipt.tokens.cache_read + timeline.receipt.tokens.cache_write)} tok</span>
              {current?.model && <span className="pill">{current.model}</span>}
            </div>
          </div>
          <div className="ctx-card">
            <div className="ctx-top">
              <span className="ctx-title">Context per step</span>
              <span className="ctx-state">
                <span className={`pip ${timeline.compactions > 0 ? "full" : ""} sm`} />
                Peaked ~{tokensHuman(timeline.peak_context)}{timeline.compactions > 0 ? ` · compacted ${timeline.compactions}×` : ""}
              </span>
            </div>
            <div className="ctx-chart">
              <div className="axis" aria-hidden="true"><span>{tokensHuman(peak)}</span><span>{tokensHuman(peak / 2)}</span><span>0</span></div>
              <div className="ctx-col">
                <div className="plot">
                  <div className="bars-ctx" role="group" aria-label="Context size after each step">
                    {bars.map((b) => (
                      <button
                        key={b.first}
                        className={`bar${b.flagged ? " fl" : ""}${b.rows.includes(highlight ?? "") ? " on" : ""}`}
                        type="button"
                        aria-label={`${b.label} · ~${tokensHuman(b.context)} context`}
                        style={{ height: `${Math.max(3, Math.round((b.context / peak) * 100))}%` }}
                        onClick={() => jump(b.target)}
                      >
                        {b.flagged && <span className="bar-flag" aria-hidden="true">!</span>}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            </div>
            <span className="hint">Black bars are flagged steps. Click any bar to jump to it in the thread.</span>
          </div>
        </header>

        <div className="thread-list" id="thread-list">
          {timeline.rows.map((r) => (
            <ThreadRow key={r.id} row={r} flagged={flagged.get(r.id)} highlighted={highlight === r.id} onFlag={() => { const o = flagged.get(r.id); if (o) { setArea(o.Area as AreaKey); setMode("issues"); } jump(r.id); }} />
          ))}
        </div>
      </section>

      <section className="analysis" aria-label="Analysis">
        <div className="a-top">
          <div className="mode-toggle" role="group" aria-label="Panel view">
            <button className={`mode${mode === "issues" ? " on" : ""}`} type="button" aria-pressed={mode === "issues"} onClick={() => setMode("issues")}>Issues · {issueCount}</button>
            <button className={`mode${mode === "receipt" ? " on" : ""}`} type="button" aria-pressed={mode === "receipt"} onClick={() => setMode("receipt")}>Receipt</button>
          </div>
          {mode === "issues" && (
            <div className="a-tabs" role="tablist" aria-label="Problem areas">
              {AREA_ORDER.map((a) => (
                <button key={a} className={`a-tab${area === a ? " on" : ""}`} role="tab" type="button" aria-selected={area === a} onClick={() => setArea(a)}>
                  {AREA_LABEL[a]}<span className="mono">{issues[a]?.length ?? 0}</span>
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="a-body">
          {mode === "receipt" ? (
            <Receipt t={timeline} onPrompt={(pid) => { const row = timeline.rows.find((r) => r.kind === "div" && r.prompt_id === pid); if (row) jump(row.id); }} />
          ) : (
            <>
              {(issues[area] ?? []).length === 0 && (
                <article className="fcard dashed">
                  <strong className="big">No {AREA_NAME[area].toLowerCase()} issues in this session</strong>
                </article>
              )}
              {(issues[area] ?? []).map((o) => {
                const key = fixKey(o);
                return (
                  <article className="fcard" key={`${o.Kind}-${o.Subject}-${o.Evidence[0]}`}>
                    <div>
                      <strong className="big">{o.Title}</strong>
                      <span className="sub">{impactLine(o)} · {o.Conf}</span>
                    </div>
                    <div className="fix-stack">
                      <p className="lbl">Evidence</p>
                      {o.Evidence.slice(0, 8).map((ev) => (
                        <button key={ev} className="ev-btn" type="button" onClick={() => jump(ev)}>
                          <span>{rowLabel(timeline.rows, ev)}</span>
                          <span>→</span>
                        </button>
                      ))}
                    </div>
                    <div className="fix-stack">
                      <p className="lbl">{o.Fix ? `Fix · ${o.FixLang === "settings-json" ? "add to settings.json" : "paste into CLAUDE.md"}` : "Habit"}</p>
                      {o.Fix ? <pre className="code">{o.Fix}</pre> : <span className="body-copy">{o.Note || o.Detail}</span>}
                      {o.Fix && o.Note && <span className="body-copy">{o.Note}</span>}
                    </div>
                    {marked[key] ? (
                      <div className="done" role="status"><strong>Marked as fixed</strong><span>Measuring for the next 7 days.</span></div>
                    ) : (
                      <div className="actions">
                        {o.Fix && (
                          <button className="btn" type="button" onClick={() => { navigator.clipboard?.writeText(o.Fix ?? "").catch(() => {}); setCopied({ ...copied, [key]: true }); }}>
                            {copied[key] ? "Copied" : "Copy"}
                          </button>
                        )}
                        <button className="btn btn-k" type="button" onClick={() => api.setFixState(key, "fixed").then(() => setMarked({ ...marked, [key]: true })).catch(() => {})}>
                          Mark as fixed
                        </button>
                      </div>
                    )}
                  </article>
                );
              })}
            </>
          )}
        </div>

        <div className="a-foot">
          <span><b>{Object.keys(marked).length}</b> of {issueCount} fixes marked</span>
          <Link className="btn" to="/fixes">Open in Fixes →</Link>
        </div>
      </section>
    </div>
  );
}

function ThreadRow({ row, flagged, highlighted, onFlag }: { row: TimelineRow; flagged?: Occurrence; highlighted: boolean; onFlag: () => void }) {
  return (
    <div className={`trow${flagged ? " fl" : ""}${highlighted ? " hl" : ""}`} id={`row-${row.id}`}>
      <div>
        {flagged && (
          <button className="flagbtn" type="button" aria-label={`Show issue in ${AREA_NAME[flagged.Area]}`} onClick={onFlag}>!</button>
        )}
      </div>
      <div className="body">
        {row.kind === "div" && (
          <div className="divider"><span className="mono">{row.text}</span><span className="rule" /><span className="mono meta-d">{row.meta}</span></div>
        )}
        {row.kind === "you" && <div className="you"><span className="lbl">You</span><p>{row.text || "(prompt not logged)"}</p></div>}
        {row.kind === "claude" && <div className="claude"><span className="lbl">Claude</span><p>{row.text}</p></div>}
        {row.kind === "tool" && (
          <div className="tool-row">
            <span>▸ <b>{row.name}</b> {row.text}{flagged && <span className="tag">{flagged.Title}</span>}{row.wait_ms ? <span className="tag">waited {Math.round(row.wait_ms / 1000)}s</span> : null}</span>
            <span className="m">{row.meta ?? ""}</span>
          </div>
        )}
        {row.kind === "event" && <div className="event"><span className="evt">{row.text}</span></div>}
      </div>
    </div>
  );
}

function Receipt({ t, onPrompt }: { t: Timeline; onPrompt: (promptID: string) => void }) {
  const r = t.receipt;
  const total = r.tokens.fresh_input + r.tokens.output + r.tokens.cache_read + r.tokens.cache_write || 1;
  const cachePct = (r.tokens.cache_read / total) * 100;
  const freshPct = (r.tokens.fresh_input / total) * 100;
  const outPct = (r.tokens.output / total) * 100;
  return (
    <article className="receipt" aria-label="Session receipt">
      <div className="rc-head">
        <div>
          <strong>SESSION RECEIPT</strong>
          <div className="rc-sub">{r.calls} model calls</div>
        </div>
        <div>
          <div className="rc-total">{money(r.cost_usd)}</div>
          <div className="rc-tokens">{tokensHuman(total)} tokens</div>
        </div>
      </div>
      <div className="rc-split">
        <div className="rc-bar" role="img" aria-label={`${Math.round(r.well_spent * 100)}% well spent`}>
          <div style={{ width: `${r.well_spent * 100}%`, background: "#000" }} />
          <div className="hatch" style={{ width: `${(1 - r.well_spent) * 100}%` }} />
        </div>
        <div className="rc-row">
          <span><b>{Math.round(r.well_spent * 100)}%</b> well spent</span>
          <span>Avoidable (est.) <b className="mono">{money(r.avoidable_usd)}</b></span>
        </div>
      </div>
      <div className="rsec">BY PROMPT</div>
      {r.prompts.map((p) => (
        <button key={p.prompt_id} className="rl" type="button" onClick={() => onPrompt(p.prompt_id)}>
          <span><b>{p.label || "(prompt not logged)"}</b><small>{tokensHuman(p.tokens)} tok · {p.flags > 0 ? `${p.flags} flag${p.flags === 1 ? "" : "s"}` : "clean"}</small></span>
          <span className="mono">{money(p.cost_usd)}</span>
        </button>
      ))}
      <div className="rsec">AVOIDABLE, BY PROBLEM</div>
      {AREA_ORDER.filter((a) => (r.by_area_usd[a] ?? 0) > 0).map((a) => (
        <div className="rl" key={a}>
          <span className="lead-item"><span className="pip full" />{AREA_LABEL[a]}</span>
          <span className="mono">{money(r.by_area_usd[a])}</span>
        </div>
      ))}
      <div className="rsec">WHERE THE TOKENS WENT</div>
      <div className="tokens-bar" role="img" aria-label={`Token mix: ${Math.round(cachePct)}% from cache, ${Math.round(freshPct)}% fresh input, ${Math.round(outPct)}% output`}>
        <div style={{ width: `${cachePct}%`, background: "#FFF" }} />
        <div style={{ width: `${freshPct}%`, background: "#000" }} />
        <div className="hatch" style={{ width: `${outPct}%` }} />
        <div style={{ width: `${(r.tokens.cache_write / total) * 100}%`, background: "#8A8A8A" }} />
      </div>
      <div className="legend-grid">
        <span><span>□ From cache</span><span className="mono">{tokensHuman(r.tokens.cache_read)}</span></span>
        <span><span>■ Fresh input</span><span className="mono">{tokensHuman(r.tokens.fresh_input)}</span></span>
        <span><span>▨ Output</span><span className="mono">{tokensHuman(r.tokens.output)}</span></span>
        <span><span><span className="soft-ink">■</span> Cache writes</span><span className="mono">{tokensHuman(r.tokens.cache_write)}</span></span>
      </div>
      <div className="torn" aria-hidden="true" />
    </article>
  );
}

function impactLine(o: Occurrence): string {
  const parts: string[] = [];
  if (o.Tokens) parts.push(`≈ ${tokensHuman(o.Tokens)} tok`);
  if (o.USD) parts.push(`≈ ${money(o.USD)}`);
  if (o.Minutes) parts.push(`${o.Minutes.toFixed(1)} min`);
  return parts.join(" · ") || "no direct cost";
}

// MAX_BARS keeps the context chart inside its column however long the session
// is. Each bar stands for a run of consecutive steps.
const MAX_BARS = 60;

type Bar = { first: string; rows: string[]; target: string; context: number; flagged: boolean; label: string };

function groupBars(rows: TimelineRow[], flagged: Map<string, Occurrence>, max: number): Bar[] {
  const size = Math.max(1, Math.ceil(rows.length / max));
  const out: Bar[] = [];
  for (let i = 0; i < rows.length; i += size) {
    const group = rows.slice(i, i + size);
    const hit = group.find((r) => flagged.has(r.id));
    out.push({
      first: group[0].id,
      rows: group.map((r) => r.id),
      target: (hit ?? group[0]).id,
      context: Math.max(...group.map((r) => r.context_tokens ?? 0)),
      flagged: Boolean(hit),
      label: group.length === 1 ? group[0].text : `${group.length} steps`,
    });
  }
  return out;
}

function rowLabel(rows: TimelineRow[], id: string): string {
  const r = rows.find((x) => x.id === id);
  if (!r) return id;
  return r.kind === "tool" ? `${r.name} ${r.text}` : r.text.slice(0, 60);
}

