import { useEffect, useMemo, useState, useCallback } from "react";
import { Link } from "react-router-dom";
import { useAgents } from "./agentContext";
import { SkippedChecks } from "./agents";
import { api, type Fixes as FixesData, type Item, type McpSetup, type Progress } from "./api";
import { slashCommands } from "./mcp";
import { CopyBlock } from "./McpSetup";
import { FirstRun } from "./FirstRun";
import { Shell } from "./Shell";
import { useFirstRun } from "./status";
import { AREA_LABEL, AREA_NAME, dayShort, money, sessionsLabel, shortPath, tokensHuman } from "./format";
import { IconCheck, IconCopy, IconMinus, IconPlus, IconX } from "./icons";
import { useRefresh } from "./refresh";

const MARK_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;

// AskClaude points to the MCP server's slash commands, which have Claude
// look further than the rules here, using the user's own Claude Code plan.
function AskClaude() {
  const [setup, setSetup] = useState<McpSetup | null>(null);
  useEffect(() => {
    api.mcpSetup().then(setSetup).catch(() => {});
  }, []);
  if (!setup) return null;
  return (
    <section className="rail-card ask-claude" aria-label="Ask Claude">
      <div className="card-top">
        <p className="eb">Ask {setup.agent_name} for more</p>
        <span className={`ok${setup.enabled ? "" : " dashed"}`}>{setup.enabled ? "On" : "Off"}</span>
      </div>
      {setup.enabled ? (
        <>
          <span className="rc-note">Run one of these in a new {setup.agent_name} session to look further than the rules here.</span>
          {slashCommands(setup.server_name).map((c) => (
            <div className="mcp-step" key={c.command}>
              <b>{c.what}</b>
              <CopyBlock text={c.command} />
            </div>
          ))}
        </>
      ) : (
        <span className="rc-note">
          Connect Token Advisor to {setup.agent_name} in <Link to="/settings">Settings</Link> to ask about this data and get more suggestions. There's no API key to add.
        </span>
      )}
    </section>
  );
}

// useFixesData loads the fixes and the week-by-week progress for both Fixes pages.
function useFixesData() {
  const [data, setData] = useState<FixesData | null>(null);
  const [progress, setProgress] = useState<Progress | null>(null);
  const [error, setError] = useState<string | null>(null);
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
  return { data, progress, error, setError, load, firstRun };
}

const DISMISS_KEY = "ta-fix-dismissed";

// Not relevant is kept in this browser for now: the API only stores fixed and
// reverted marks.
function readDismissed(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(DISMISS_KEY) ?? "[]");
    return Array.isArray(v) ? v.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}

const TABS = [
  { value: "open", label: "To fix" },
  { value: "marked", label: "Marked fixed" },
  { value: "dismissed", label: "Not relevant" },
] as const;
type Tab = (typeof TABS)[number]["value"];
const AREA_FILTERS = ["all", "context", "perm", "exec", "model"] as const;
type AreaFilter = (typeof AREA_FILTERS)[number];
const WHERE: Record<string, string> = { "claude-md": "Paste into CLAUDE.md", "settings-json": "Paste into .claude/settings.json", bash: "Run in a terminal" };
const HABITS_SHOWN = 3;

export default function Fixes() {
  const { data, progress, error, setError, load, firstRun } = useFixesData();
  const [copied, setCopied] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("open");
  const [areaF, setAreaF] = useState<AreaFilter>("all");
  const [openKey, setOpenKey] = useState<string | null | undefined>(undefined);
  const [dismissed, setDismissedState] = useState<string[]>(readDismissed);
  const [allHabits, setAllHabits] = useState(false);
  // now is when the page loaded, which is precise enough for a 7-day window.
  const [now] = useState(Date.now);

  const setDismissed = (keys: string[]) => {
    setDismissedState(keys);
    try { localStorage.setItem(DISMISS_KEY, JSON.stringify(keys)); } catch { /* storage unavailable */ }
  };

  const groups = useMemo(() => groupFixes(data?.to_fix ?? []), [data]);
  const isDismissed = (g: FixGroup) => g.items.every((it) => dismissed.includes(it.key));
  const openGroups = groups.filter((g) => !isDismissed(g));
  const dismissedGroups = groups.filter(isDismissed);
  const byArea = <T extends { area: string }>(list: T[]) => (areaF === "all" ? list : list.filter((x) => x.area === areaF));
  const habitGroups = useMemo(() => groupHabits(data?.habits ?? []), [data]);
  // The first card starts open, as in the design; a closed one stays closed.
  const expanded = openKey === undefined ? byArea(openGroups)[0]?.key ?? null : openKey;

  const mark = async (items: Item[], state: "fixed" | "reverted") => {
    setBusy(items[0].key);
    try {
      for (const it of items) await api.setFixState(it.key, state);
      load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  // Only cards with a fix show Copy.
  const copy = (g: FixGroup) => {
    navigator.clipboard?.writeText(g.fix ?? "").catch(() => {});
    setCopied({ ...copied, [g.key]: true });
  };

  const weeks = progress?.weeks ?? [];
  const first = weeks[0];
  const last = weeks[weeks.length - 1];
  const saved = first && last ? first.avoidable_usd - last.avoidable_usd : 0;
  const leftUsd = openGroups.reduce((n, g) => n + g.usd, 0);
  const leftMin = Math.round(openGroups.reduce((n, g) => n + g.minutes, 0));
  const counts: Record<Tab, number> = { open: openGroups.length, marked: data?.counts.marked ?? 0, dismissed: dismissedGroups.length };

  if (firstRun) return <Shell><FirstRun /></Shell>;

  return (
    <Shell fixesToDo={data ? openGroups.length : undefined} sessionCount={undefined}>
      <div className="fixes-head">
        <div className="title-block">
          <h1 className="title">Fixes</h1>
          <p className="sub">{data ? `${openGroups.length} to fix · ${data.counts.marked} marked fixed · ordered by impact` : "Loading…"}</p>
        </div>
        <Link className="btn" to="/fixes/progress">See your progress report →</Link>
      </div>

      {error && <p className="hint-line">Could not load: {error}</p>}
      <SkippedChecks />

      {data && (
        <>
          <div className="toolbar fx-toolbar">
            <div className="vtabs" role="tablist" aria-label="Fix state">
              {TABS.map((x) => (
                <button key={x.value} className={`vt${tab === x.value ? " on" : ""}`} role="tab" type="button" aria-selected={tab === x.value} onClick={() => setTab(x.value)}>
                  {x.label}<span className="cnt">{counts[x.value]}</span>
                </button>
              ))}
            </div>
            <div className="area-pills" role="group" aria-label="Problem area">
              {AREA_FILTERS.map((a) => (
                <button key={a} type="button" className={`apill${areaF === a ? " on" : ""}`} aria-pressed={areaF === a} onClick={() => setAreaF(a)}>
                  {a === "all" ? "All" : AREA_LABEL[a]}
                </button>
              ))}
            </div>
          </div>

          <div className="fx-layout">
            <div className="fx-main">
              {tab === "open" && (
                <>
                  <p className="fx-help">Sorted by what you'd recover. Open one to copy the fix.</p>
                  {byArea(openGroups).length === 0 && <p className="hint-line">Nothing with a config fix right now.</p>}
                  {byArea(openGroups).map((g) => {
                    const isOpen = expanded === g.key;
                    return (
                      <article className={`fx-card${isOpen ? " open" : ""}`} key={g.key}>
                        <button type="button" className="fx-top" aria-expanded={isOpen} onClick={() => setOpenKey(isOpen ? null : g.key)}>
                          <span className={`pip ${g.severity}`} aria-hidden="true" />
                          <span className="fx-t">
                            <strong>{shortPath(g.title)}</strong>
                            <span className="fx-meta"><span className="chip">{AREA_NAME[g.area]}</span><span>{g.items.length > 1 ? `${g.items.length} findings · ` : ""}{sessionsLabel(g.sessions)}</span></span>
                          </span>
                          <span className="fx-impact">
                            {impactMain(g) && <span className="mono">{impactMain(g)}</span>}
                            {impactSub(g) && <small>{impactSub(g)}</small>}
                          </span>
                          <span className="fx-tog" aria-hidden="true">{isOpen ? <IconMinus size={16} /> : <IconPlus size={16} />}</span>
                        </button>
                        {isOpen && (
                          <div className="fx-body">
                            <div className="fx-detail">
                              {g.detail && <span className="body-copy">{shortPath(g.detail)}</span>}
                              <span className="conf">{g.conf}</span>
                            </div>
                            {g.items.length > 1 && <span className="fx-subjects">Seen with: {g.items.slice(0, 6).map((it) => shortPath(it.subject)).join(", ")}{g.items.length > 6 ? ` +${g.items.length - 6} more` : ""}</span>}
                            {g.fix && (
                              <>
                                <p className="lbl">{WHERE[g.fix_lang ?? ""] ?? "Fix"}</p>
                                <pre className="code">{shortPath(g.fix)}</pre>
                              </>
                            )}
                            {g.note && <span className="note">{g.note}</span>}
                            <div className="acts">
                              {g.fix && <button className="btn" type="button" onClick={() => copy(g)}><IconCopy size={16} />{copied[g.key] ? "Copied" : "Copy"}</button>}
                              <button className="btn btn-k" type="button" disabled={busy === g.items[0].key} onClick={() => mark(g.items, "fixed")}><IconCheck size={16} />Mark as fixed</button>
                              <button className="btn btn-q" type="button" onClick={() => setDismissed([...new Set([...dismissed, ...g.items.map((it) => it.key)])])}><IconX size={14} /><span className="u">Not relevant</span></button>
                            </div>
                          </div>
                        )}
                      </article>
                    );
                  })}
                </>
              )}

              {tab === "marked" && (
                <>
                  {byArea(data.marked).length === 0 && <p className="hint-line">Nothing marked yet. Mark a fix once you have applied it.</p>}
                  {byArea(data.marked).map((it) => {
                    const measuring = it.marked_at !== undefined && now - it.marked_at < MARK_WINDOW_MS;
                    return (
                      <article className="card dashed" key={it.key}>
                        <div className="card-top">
                          <span className="chip">{AREA_NAME[it.area]}</span>
                          <span className="ok dashed">{measuring ? "Measuring" : "Measured"}</span>
                        </div>
                        <strong>{shortPath(it.title)}</strong>
                        <span className="sub-line">Marked {it.marked_at ? dayShort(it.marked_at) : ""}</span>
                        <button className="btn btn-q linkish" type="button" disabled={busy === it.key} onClick={() => mark([it], "reverted")}>
                          Not fixed after all? Move back
                        </button>
                      </article>
                    );
                  })}
                </>
              )}

              {tab === "dismissed" && (
                <>
                  {byArea(dismissedGroups).length === 0 && <p className="hint-line">Nothing set aside. Use “Not relevant” on a fix that doesn't apply to you.</p>}
                  {byArea(dismissedGroups).map((g) => (
                    <article className="card dashed" key={g.key}>
                      <span className="chip">{AREA_NAME[g.area]}</span>
                      <strong>{shortPath(g.title)}</strong>
                      <button className="btn btn-q linkish" type="button" onClick={() => setDismissed(dismissed.filter((k) => !g.items.some((it) => it.key === k)))}>
                        Move back to To fix
                      </button>
                    </article>
                  ))}
                </>
              )}
            </div>

            <aside className="fx-rail" aria-label="Summary">
              <section className="rail-card recover">
                <p className="eb">Left to recover</p>
                <div className="rc-big">≈ {money(leftUsd)}{leftMin > 0 ? ` + ${leftMin} min waiting` : ""}</div>
                <span className="rc-note">if you fix the {openGroups.length} open {openGroups.length === 1 ? "item" : "items"} (estimated)</span>
              </section>
              <section className="rail-card working">
                <p className="eb">Already working</p>
                <div className="rc-big">{saved > 0.005 ? `${money(saved)} saved` : `${data.counts.marked} marked fixed`}</div>
                <span className="rc-note">{saved > 0.005 ? "less avoidable spend a week than your first week" : "each is measured for 7 days after you mark it"}</span>
                <Link to="/fixes/progress">Full progress report →</Link>
              </section>
              <AskClaude />
              {habitGroups.length > 0 && (
                <section className="rail-card habits-card">
                  <p className="eb">Habits · no config needed</p>
                  {(allHabits ? habitGroups : habitGroups.slice(0, HABITS_SHOWN)).map((h) => (
                    <p className="habit-line" key={h.key}><b>{shortPath(h.title)}.</b> {h.note}</p>
                  ))}
                  {habitGroups.length > HABITS_SHOWN && (
                    <button type="button" className="btn btn-q linkish" onClick={() => setAllHabits(!allHabits)}>
                      {allHabits ? "Show fewer" : `Show all ${habitGroups.length}`}
                    </button>
                  )}
                </section>
              )}
            </aside>
          </div>
        </>
      )}
    </Shell>
  );
}

// FixesProgress is the progress report: how avoidable spend and healthy
// sessions moved since the first week.
export function FixesProgress() {
  const { data, progress, error, firstRun } = useFixesData();
  const weeks = progress?.weeks ?? [];
  const first = weeks[0];
  const last = weeks[weeks.length - 1];
  const down = first && last && first.avoidable_usd > 0 ? Math.round((1 - last.avoidable_usd / first.avoidable_usd) * 100) : null;
  const installWide = data?.to_fix.filter((it) => it.sessions === 0).length ?? 0;
  const maxAvoid = Math.max(1, ...weeks.map((w) => w.avoidable_usd));

  if (firstRun) return <Shell><FirstRun /></Shell>;

  return (
    <Shell fixesToDo={data?.to_fix.length} sessionCount={undefined}>
      <div className="fixes-head">
        <div className="title-block">
          <p className="eb"><Link className="crumb" to="/fixes">← Fixes</Link> · your progress</p>
          <h1>{down !== null && down > 0 ? `Avoidable spend is down ${down}% since you started.` : "Avoidable spend since you started."}</h1>
          <p className="sub">Compared with your first week of sessions. All figures are estimated from your own sessions.</p>
        </div>
      </div>

      {error && <p className="hint-line">Could not load: {error}</p>}
      {!data && !error && <p className="hint-line">Loading…</p>}

      {data && (
        <section className="progress" aria-label="Your progress">
          <div className="stats">
            <div className="stat-main">
              <p className="eb">Session health</p>
              <div className="num-line">
                <span className="num">{last ? `${Math.round(last.healthy_pct * 100)}%` : "—"}</span>
                <span className="num-label">healthy sessions</span>
              </div>
              {installWide > 0 && (
                <p className="stat-note">Counts problems found inside sessions. {installWide} global {installWide === 1 ? "fix" : "fixes"} below {installWide === 1 ? "isn't" : "aren't"} included.</p>
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
              {weeks.map((w, i) => (
                <span key={w.week}>{i % Math.max(1, Math.ceil(weeks.length / 8)) === 0 ? `${dayShort(Date.parse(w.week))}${w.marked_fixes > 0 ? ` · ${w.marked_fixes} marked` : ""}` : ""}</span>
              ))}
            </div>
          </div>
        </section>
      )}
    </Shell>
  );
}

type FixGroup = {
  key: string;
  area: Item["area"];
  items: Item[];
  title: string;
  severity: "full" | "half";
  sessions: number;
  usd: number;
  minutes: number;
  tokens: number;
  score: number;
  conf: string;
  detail: string;
  fix?: string;
  fix_lang?: string;
  note?: string;
};

// groupFixes merges findings that share one copy-paste fix (the same CLAUDE.md
// lines for six re-read files) into one card, biggest impact first. Findings
// with no measurable impact go last.
function groupFixes(items: Item[]): FixGroup[] {
  const by = new Map<string, Item[]>();
  for (const it of items) {
    const k = `${it.area}|${it.fix ?? it.key}`;
    by.set(k, [...(by.get(k) ?? []), it]);
  }
  const out: FixGroup[] = [];
  for (const [key, list] of by) {
    const lead = [...list].sort((a, b) => b.score - a.score)[0];
    const bullet = /^\s*-\s+(.+?)\.?\s*$/m.exec(lead.fix ?? "")?.[1];
    out.push({
      key,
      area: lead.area,
      items: list,
      title: list.length > 1 && bullet ? bullet : lead.title,
      severity: list.some((it) => it.severity === "full") ? "full" : "half",
      sessions: Math.max(...list.map((it) => it.sessions)),
      usd: list.reduce((n, it) => n + (it.usd ?? 0), 0),
      minutes: list.reduce((n, it) => n + (it.minutes ?? 0), 0),
      tokens: list.reduce((n, it) => n + (it.tokens ?? 0), 0),
      score: list.reduce((n, it) => n + it.score, 0),
      conf: lead.conf,
      detail: list.length > 1 ? `${list.length} findings share this fix. ${lead.detail}` : lead.detail,
      fix: lead.fix,
      fix_lang: lead.fix_lang,
      note: lead.note,
    });
  }
  const empty = (g: FixGroup) => g.usd < 0.005 && g.minutes < 0.05 && g.tokens < 1;
  return out.sort((a, b) => Number(empty(a)) - Number(empty(b)) || b.score - a.score);
}

type HabitGroup = { key: string; title: string; note: string; score: number };

// groupHabits folds habits with the same advice into one line.
function groupHabits(items: Item[]): HabitGroup[] {
  const by = new Map<string, Item[]>();
  for (const it of items) {
    const note = it.note || it.detail;
    by.set(note, [...(by.get(note) ?? []), it]);
  }
  return [...by.entries()]
    .map(([note, list]) => {
      const lead = [...list].sort((a, b) => b.score - a.score)[0];
      return {
        key: lead.key,
        title: list.length > 1 ? `${lead.title.replace(/\.$/, "")}, and ${list.length - 1} more like it` : lead.title.replace(/\.$/, ""),
        note,
        score: list.reduce((n, it) => n + it.score, 0),
      };
    })
    .sort((a, b) => b.score - a.score);
}

function impactMain(g: FixGroup): string {
  if (g.tokens > 0) return `≈ ${tokensHuman(g.tokens)} tok`;
  if (g.usd >= 0.005) return `≈ ${money(g.usd)}`;
  if (g.minutes >= 0.05) return `${Math.round(g.minutes) || "<1"} min`;
  return "";
}

function impactSub(g: FixGroup): string {
  if (g.tokens > 0 && g.usd >= 0.005) return `≈ ${money(g.usd)}`;
  if (g.minutes >= 0.05 && (g.tokens > 0 || g.usd >= 0.005)) return `${Math.round(g.minutes)} min waiting`;
  if (g.minutes >= 0.05) return "waiting";
  return "";
}
