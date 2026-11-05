import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Shell, Sidebar } from "./Shell";
import { api, type Occurrence, type SessionCard, type Timeline, type TimelineRow } from "./api";
import { Markdown } from "./Markdown";
import { AREA_LABEL, AREA_NAME, cleanTitle, dayShort, durationS, isClaude, money, shortPath, tokensHuman } from "./format";
import { AgentAvatar, IconCheck, IconChevronDown, IconChevronUp, IconCopy, IconSearch, YouAvatar } from "./icons";

type Mode = "issues" | "receipt";
type AreaKey = "context" | "exec" | "perm" | "model";
const AREA_ORDER: AreaKey[] = ["model", "context", "exec", "perm"];

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
  const [chartOpen, setChartOpen] = useState(true);

  // App.tsx keys this page by id, so another session starts from empty state.
  useEffect(() => {
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

  if (error) {
    return (
      <Shell>
        <div className="empty">
          <strong>{/not found/i.test(error) ? "Session not found" : "Could not load this session"}</strong>
          <span>{/not found/i.test(error) ? "It may have been purged, or the link is wrong." : error}</span>
          <Link className="btn" to="/sessions">← Back to Sessions</Link>
        </div>
      </Shell>
    );
  }
  if (!timeline) return <div className="shell"><p className="hint-line">Loading…</p></div>;

  const peak = Math.max(1, timeline.peak_context);
  const rows = timeline.rows ?? [];
  const ctxRows = rows.filter((r) => typeof r.context_tokens === "number");
  const bars = groupBars(ctxRows, flagged, MAX_BARS);
  const promptCount = rows.filter((r) => r.kind === "div").length;
  const turnStarts = agentTurnStarts(rows);
  const health = worst(Object.values(issues).flat());
  const axis = promptLabels(bars, rows);
  const jump = (rowId: string) => {
    setHighlight(rowId);
    document.getElementById(`row-${rowId}`)?.scrollIntoView({ block: "center", behavior: "smooth" });
  };
  const fixKey = (o: Occurrence) => `${o.Kind}|${o.Subject}`;
  const areaIssues = issues[area] ?? [];
  const areaHealth = worst(areaIssues);

  return (
    <div className="shell session-shell">
      <Sidebar currentSessionId={id} />

      <section className="thread" aria-label="Session thread">
        <header className="thread-head">
          <div className="thread-title-block">
            <p className="eb"><Link className="crumb" to="/sessions">← Sessions</Link>{current?.project ? <> / {current.project}</> : null}{promptCount > 0 && <> · {promptCount} {promptCount === 1 ? "prompt" : "prompts"}</>}</p>
            <h1 className={`thread-title${current ? " q" : ""}`} title={current?.title}>{current ? cleanTitle(current.title) : (rows.length === 0 ? "Session with no activity" : id)}</h1>
            <div className="meta-row">
              <span className="pill">{dayShort(timeline.started_at)}</span>
              <span className="pill">{durationS(current?.duration_s ?? 0)} active</span>
              <span className="pill strong">{timeline.cost_unknown ? "—" : money(timeline.receipt.cost_usd)}</span>
              <span className="pill">{tokensHuman(timeline.receipt.tokens.fresh_input + timeline.receipt.tokens.output + timeline.receipt.tokens.cache_read + timeline.receipt.tokens.cache_write)} tok</span>
              {current?.model && <span className="pill">{current.model}</span>}
            </div>
          </div>
          <div className="ctx-card">
            <div className="ctx-top">
              <span className="ctx-title">Context per step</span>
              {health && <span className={`hbadge ${health}`}>{health === "full" ? "Unhealthy" : "Worth a look"}</span>}
              <span className="ctx-state mono">
                peak ~{tokensHuman(timeline.peak_context)}{timeline.compactions > 0 ? ` · compacted ${timeline.compactions === 1 ? "once" : `${timeline.compactions}×`}` : ""}
              </span>
              <button type="button" className="ctx-hide" aria-expanded={chartOpen} onClick={() => setChartOpen(!chartOpen)}>
                {chartOpen ? <>Hide<IconChevronUp size={16} /></> : <>Show<IconChevronDown size={16} /></>}
              </button>
            </div>
            {chartOpen && (
              <>
                <div className="ctx-chart">
                  <div className="axis" aria-hidden="true"><span>{tokensHuman(peak)}</span><span>{tokensHuman(peak / 2)}</span><span>0</span></div>
                  <div className="ctx-col">
                    <div className="plot">
                      <div className="gridline" style={{ top: 0 }} />
                      <div className="gridline" style={{ top: "50%" }} />
                      <div className="bars-ctx" role="group" aria-label="Context size after each step">
                        {bars.map((b, i) => (
                          <button
                            key={b.first}
                            className={`bar${b.flagged ? ` fl ${b.severity}` : ""}${b.rows.includes(highlight ?? "") ? " on" : ""}${i > 0 && axis.starts.has(i) ? " pstart" : ""}`}
                            type="button"
                            aria-label={`${b.label} · ~${tokensHuman(b.context)} context`}
                            style={{ height: `${Math.max(3, Math.round((b.context / peak) * 100))}%` }}
                            onClick={() => jump(b.target)}
                          />
                        ))}
                      </div>
                    </div>
                    {axis.labels.length > 0 && (
                      <div className="prompt-axis" aria-hidden="true">
                        {axis.labels.map((l) => <span key={l.n} style={{ left: `${l.left}%` }}>PROMPT {l.n}</span>)}
                      </div>
                    )}
                  </div>
                </div>
                <div className="hint">
                  <span className="key now" />Fix now<span className="key look" />Worth a look<span className="hint-t">Click a bar to jump to that step.</span>
                </div>
              </>
            )}
          </div>
        </header>

        <div className="thread-list" id="thread-list">
          {rows.length === 0 && <p className="hint-line">Nothing was recorded in this session: no prompts, replies or tool calls.</p>}
          {rows.map((r) => (
            <ThreadRow key={r.id} row={r} turnStart={turnStarts.has(r.id)} agentName={timeline.agent_name} model={current?.model} flagged={flagged.get(r.id)} highlighted={highlight === r.id} onFlag={() => { const o = flagged.get(r.id); if (o) { setArea(o.Area as AreaKey); setMode("issues"); } jump(r.id); }} />
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
                <button key={a} className={`a-tab area-${a}${area === a ? " on" : ""}`} role="tab" type="button" aria-selected={area === a} onClick={() => setArea(a)}>
                  <span className="adot" aria-hidden="true" />{AREA_LABEL[a]}<span className="mono">{issues[a]?.length ?? 0}</span>
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="a-body">
          {mode === "receipt" ? (
            <Receipt t={timeline} project={current?.project} duration={current?.duration_s} model={current?.model} onPrompt={(pid) => { const row = rows.find((r) => r.kind === "div" && r.prompt_id === pid); if (row) jump(row.id); }} />
          ) : (
            <div className={`area-panel area-${area}`}>
              <div className="ap-head">
                <span className="ap-ico" aria-hidden="true"><IconSearch size={18} /></span>
                <h2>{AREA_NAME[area]}</h2>
                <span className={`hbadge ${areaHealth ?? "ok"}`}>{areaHealth === "full" ? "Unhealthy" : areaHealth === "half" ? "Worth a look" : "Healthy"}</span>
              </div>
              {areaIssues.length === 0 && (
                <article className="fcard dashed">
                  <strong className="big">No {AREA_NAME[area].toLowerCase()} issues in this session</strong>
                </article>
              )}
              {areaIssues.map((o) => {
                const key = fixKey(o);
                return (
                  <article className="fcard" key={`${o.Kind}-${o.Subject}-${o.Evidence[0]}`}>
                    <div>
                      <strong className="big">{shortPath(o.Title)}</strong>
                      <span className="sub">{impactLine(o)} · {o.Conf}</span>
                    </div>
                    <div className="fix-stack">
                      <p className="lbl">Evidence</p>
                      {o.Evidence.slice(0, 8).map((ev) => (
                        <button key={ev} className={`ev-btn${highlight === ev ? " on" : ""}`} type="button" onClick={() => jump(ev)}>
                          <span>{shortPath(rowLabel(rows, ev))}</span>
                          <span>→</span>
                        </button>
                      ))}
                    </div>
                    <div className="fix-stack">
                      <p className="lbl">{o.Fix ? `Fix · ${o.FixWhere}` : "Habit"}</p>
                      {o.Fix ? <pre className="code">{shortPath(o.Fix)}</pre> : <span className="body-copy">{o.Note || o.Detail}</span>}
                      {o.Fix && o.Note && <span className="body-copy">{o.Note}</span>}
                    </div>
                    {marked[key] ? (
                      <div className="done" role="status"><strong>Marked as fixed</strong><span>Measuring for the next 7 days.</span></div>
                    ) : (
                      <div className="actions">
                        {o.Fix && (
                          <button className="btn" type="button" onClick={() => { navigator.clipboard?.writeText(o.Fix ?? "").catch(() => {}); setCopied({ ...copied, [key]: true }); }}>
                            <IconCopy size={16} />{copied[key] ? "Copied" : "Copy"}
                          </button>
                        )}
                        <button className="btn btn-k" type="button" onClick={() => api.setFixState(key, "fixed").then(() => setMarked({ ...marked, [key]: true })).catch(() => {})}>
                          <IconCheck size={16} />Mark as fixed
                        </button>
                      </div>
                    )}
                  </article>
                );
              })}
            </div>
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

// A prompt longer than this many characters starts collapsed.
const LONG_PROMPT = 600;

function YouBubble({ text }: { text: string }) {
  const long = text.length > LONG_PROMPT;
  const [open, setOpen] = useState(false);
  return (
    <div className="you">
      <div className="who"><YouAvatar /><b>You</b></div>
      <p className={long && !open ? "clamp" : undefined}>{text || "(prompt not logged)"}</p>
      {long && <button type="button" className="more" onClick={() => setOpen(!open)}>{open ? "Show less" : "Show more"}</button>}
    </div>
  );
}

function ThreadRow({ row, turnStart, agentName, model, flagged, highlighted, onFlag }: { row: TimelineRow; turnStart: boolean; agentName: string; model?: string; flagged?: Occurrence; highlighted: boolean; onFlag: () => void }) {
  const waited = row.kind === "tool" && (row.wait_ms ?? 0) > 0;
  return (
    <div className={`trow${flagged ? " fl" : ""}${highlighted ? " hl" : ""}`} id={`row-${row.id}`}>
      <div className="gutter">
        {flagged && (
          <button className="flagbtn" type="button" aria-label={`Show issue in ${AREA_NAME[flagged.Area]}`} onClick={onFlag}>!</button>
        )}
      </div>
      <div className="body">
        {row.kind === "div" && (
          <div className="divider"><span className="mono">{row.text}</span><span className="rule" /><span className="mono meta-d">{row.meta}</span></div>
        )}
        {row.kind === "you" && <YouBubble text={row.text} />}
        {row.kind === "claude" && (
          <div className="claude">
            <AgentWho agentName={agentName} model={model} />
            <Markdown text={row.text} />
          </div>
        )}
        {turnStart && <div className="claude turn-head"><AgentWho agentName={agentName} model={model} /></div>}
        {row.kind === "tool" && waited && (
          <div className="wait-row">
            <span className="wl"><span className="pause" aria-hidden="true">‖</span><span className="wl-k">Waiting on you ·</span><b>{row.name}</b>{row.text ? <span className="args">({shortPath(row.text)})</span> : null}<span className="tag red">Approved by hand</span>{flagged && <span className="tag red">{flagTag(flagged)}</span>}</span>
            <span className="ws">{Math.round((row.wait_ms ?? 0) / 1000)}s</span>
          </div>
        )}
        {row.kind === "tool" && !waited && (
          <div className="tool-row">
            <span className="tl"><span className="tdot" aria-hidden="true" /><b>{row.name}</b>{row.text ? <span className="args">({shortPath(row.text)})</span> : null}{flagged && <span className="tag red">{flagTag(flagged)}</span>}</span>
            <span className="m">{(row.meta ?? "").replace(/^≈\s*/, "")}</span>
          </div>
        )}
        {row.kind === "event" && (
          <div className="event"><b className="evt">{row.text.replace(/^Context compacted/, "Conversation compacted")}</b>{flagged && <span className="tag">{flagged.Conf}</span>}</div>
        )}
      </div>
    </div>
  );
}

function AgentWho({ agentName, model }: { agentName: string; model?: string }) {
  return <div className="who"><AgentAvatar name={agentName} /><b>{isClaude(agentName) ? "Claude" : agentName}</b><span className="who-m">{[model, agentName].filter(Boolean).join(" · ")}</span></div>;
}

// agentTurnStarts finds the steps that open the agent's turn without a reply
// first, such as a turn that starts with tool calls. Those steps carry the
// agent's name, so every turn shows who answered.
function agentTurnStarts(rows: TimelineRow[]): Set<string> {
  const starts = new Set<string>();
  let named = true;
  for (const r of rows) {
    if (r.kind === "you") named = false;
    else if (r.kind === "claude") named = true;
    else if (r.kind !== "div" && !named) {
      starts.add(r.id);
      named = true;
    }
  }
  return starts;
}

// flagTag is the short badge on a flagged step, from the finding's kind:
// "re_read" -> "RE-READ".
function flagTag(o: Occurrence): string {
  return (o.Kind || o.Title).replace(/_/g, "-");
}

// worst is the most severe level among some findings, or null for none.
function worst(list: Occurrence[]): "full" | "half" | null {
  if (list.some((o) => o.Severity === "full")) return "full";
  return list.length > 0 ? "half" : null;
}

// promptLabels finds the bars where a new prompt starts and labels them under
// the chart. Prompts too close to the last label get neither a label nor a
// divider, so a session with many short prompts stays readable.
function promptLabels(bars: Bar[], rows: TimelineRow[]): { starts: Set<number>; labels: { n: number; left: number }[] } {
  const promptOf = new Map(rows.map((r) => [r.id, r.prompt_id ?? ""]));
  const order = rows.filter((r) => r.kind === "div").map((r) => r.prompt_id ?? "");
  const starts = new Set<number>();
  const labels: { n: number; left: number }[] = [];
  let prev = "";
  bars.forEach((b, i) => {
    const p = promptOf.get(b.first) ?? "";
    if (p && p !== prev) {
      const left = (i / Math.max(1, bars.length)) * 100;
      const last = labels[labels.length - 1];
      if (!last || left - last.left >= 14) {
        labels.push({ n: order.indexOf(p) + 1 || labels.length + 1, left });
        starts.add(i);
      }
    }
    prev = p || prev;
  });
  return { starts, labels };
}

// The receipt lists this many prompts at first, and this many more per click.
const RECEIPT_PROMPTS = 5;

function moreLabel(left: number): string {
  return left > RECEIPT_PROMPTS ? `Show ${RECEIPT_PROMPTS} more of ${left}` : `Show ${left} more`;
}

function Receipt({ t, project, duration, model, onPrompt }: { t: Timeline; project?: string; duration?: number; model?: string; onPrompt: (promptID: string) => void }) {
  const r = t.receipt;
  const total = r.tokens.fresh_input + r.tokens.output + r.tokens.cache_read + r.tokens.cache_write || 1;
  const cachePct = (r.tokens.cache_read / total) * 100;
  const freshPct = (r.tokens.fresh_input / total) * 100;
  const outPct = (r.tokens.output / total) * 100;
  const byArea = r.by_area_usd ?? {};
  const avoidableAreas = AREA_ORDER.filter((a) => (byArea[a] ?? 0) > 0);
  // Prompts keep their number in the session but are listed costliest first.
  const prompts = (r.prompts ?? []).map((p, i) => ({ p, n: i + 1 })).sort((a, b) => b.p.cost_usd - a.p.cost_usd);
  const [shown, setShown] = useState(RECEIPT_PROMPTS);
  return (
    <article className="receipt" aria-label="Session receipt">
      <div className="rc-head">
        <div>
          <strong>SESSION RECEIPT</strong>
          <div className="rc-sub">{[project, dayShort(t.started_at), duration ? durationS(duration) : "", model].filter(Boolean).join(" · ")}</div>
        </div>
        <div>
          <div className="rc-total">{t.cost_unknown ? "—" : money(r.cost_usd)}</div>
          <div className="rc-tokens">{tokensHuman(total)} tokens</div>
        </div>
      </div>
      <div className="rc-split">
        <div className="rc-bar" role="img" aria-label={`${Math.round(r.well_spent * 100)}% well spent`}>
          <div className="seg-spent" style={{ width: `${r.well_spent * 100}%` }} />
          <div className="hatch" style={{ width: `${(1 - r.well_spent) * 100}%` }} />
        </div>
        <div className="rc-row">
          <span className="ws-pct"><b>{Math.round(r.well_spent * 100)}% well spent</b></span>
          <span>Avoidable (est.) <b className="mono av">{money(r.avoidable_usd)}</b></span>
        </div>
      </div>
      <div className="rsec">BY PROMPT</div>
      {prompts.slice(0, shown).map(({ p, n }) => (
        <button key={p.prompt_id} className="rl" type="button" onClick={() => onPrompt(p.prompt_id)}>
          <span><b>{n} · {p.label ? cleanTitle(p.label) : "(prompt not logged)"}</b><small>{tokensHuman(p.tokens)} tok · {p.flags > 0 ? `${p.flags} flag${p.flags === 1 ? "" : "s"}` : "clean"}</small></span>
          <span className="mono">{money(p.cost_usd)}</span>
        </button>
      ))}
      {prompts.length > RECEIPT_PROMPTS && (
        <button type="button" className="more rc-more" onClick={() => setShown(shown < prompts.length ? shown + RECEIPT_PROMPTS : RECEIPT_PROMPTS)}>
          {shown < prompts.length ? moreLabel(prompts.length - shown) : "Show less"}
        </button>
      )}
      {avoidableAreas.length > 0 && (
        <>
          <div className="rsec">AVOIDABLE, BY PROBLEM</div>
          {avoidableAreas.map((a) => (
            <div className="rl" key={a}>
              <span className={`lead-item area-${a}`}><span className="adot" />{AREA_NAME[a]}</span>
              <span className="mono">{money(byArea[a])}</span>
            </div>
          ))}
        </>
      )}
      <div className="rsec">WHERE THE TOKENS WENT</div>
      <div className="tokens-bar" role="img" aria-label={`Token mix: ${Math.round(cachePct)}% from cache, ${Math.round(freshPct)}% fresh input, ${Math.round(outPct)}% output`}>
        <div className="seg-cache" style={{ width: `${cachePct}%` }} />
        <div className="seg-fresh" style={{ width: `${freshPct}%` }} />
        <div className="hatch" style={{ width: `${outPct}%` }} />
        <div className="seg-write" style={{ width: `${(r.tokens.cache_write / total) * 100}%` }} />
      </div>
      <div className="legend-grid">
        <span><span><span className="g-cache">□</span> From cache</span><span className="mono">{tokensHuman(r.tokens.cache_read)}</span></span>
        <span><span><span className="g-fresh">■</span> Fresh input</span><span className="mono">{tokensHuman(r.tokens.fresh_input)}</span></span>
        <span><span><span className="g-out">▨</span> Output</span><span className="mono">{tokensHuman(r.tokens.output)}</span></span>
        <span><span><span className="g-write">■</span> Cache writes</span><span className="mono">{tokensHuman(r.tokens.cache_write)}</span></span>
      </div>
      <div className="receipt-foot">{r.calls} model calls</div>
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

type Bar = { first: string; rows: string[]; target: string; context: number; flagged: boolean; severity: "full" | "half"; label: string };

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
      severity: group.some((r) => flagged.get(r.id)?.Severity === "full") ? "full" : "half",
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

