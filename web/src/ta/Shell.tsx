import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Link, NavLink } from "react-router-dom";
import { useAgents } from "./agentContext";
import { AgentPicker } from "./agents";
import { api, type SessionCard } from "./api";
import { cleanTitle, money } from "./format";
import { IconChat, IconChevronDown, IconFolder, IconGrid, IconMoon, IconSettings, IconSun, IconWrench, Logo } from "./icons";
import { PERIODS, useRange } from "./range";
import { useRefresh } from "./refresh";
import { useStatus } from "./status";
import { useTheme } from "./theme";

// Shell is the sidebar layout shared by Overview, Sessions, Fixes and Settings.
export function Shell({ children, fixesToDo, sessionCount }: { children: ReactNode; fixesToDo?: number; sessionCount?: number }) {
  return (
    <div className="shell">
      <Sidebar fixesToDo={fixesToDo} sessionCount={sessionCount} />
      <main className="main">{children}</main>
    </div>
  );
}

const SIDE_SORTS = [
  { value: "priority", label: "Priority" },
  { value: "newest", label: "Newest" },
  { value: "cost", label: "Cost" },
] as const;
type SideSort = (typeof SIDE_SORTS)[number]["value"];
const SORT_KEY = "ta-side-sort";
const RECENTS_KEY = "ta-recents-open";
const RANK: Record<string, number> = { now: 0, look: 1, fine: 2 };
// The sidebar lists a few sessions per project, for the busiest projects only.
const MAX_PROJECTS = 5;
const MAX_PER_PROJECT = 3;

function readSort(): SideSort {
  try {
    const v = localStorage.getItem(SORT_KEY);
    return SIDE_SORTS.some((s) => s.value === v) ? (v as SideSort) : "priority";
  } catch {
    return "priority";
  }
}

function readOpen(): boolean {
  try {
    return localStorage.getItem(RECENTS_KEY) !== "0";
  } catch {
    return true;
  }
}

export function Sidebar({ fixesToDo, sessionCount, currentSessionId }: { fixesToDo?: number; sessionCount?: number; currentSessionId?: string }) {
  const status = useStatus();
  const [aiOn, setAiOn] = useState(false);
  const [theme, toggleTheme] = useTheme();
  const [range, setRange] = useRange();
  const [sort, setSortState] = useState<SideSort>(readSort);
  const [open, setOpenState] = useState(readOpen);
  const [cards, setCards] = useState<SessionCard[] | null>(null);
  const { selected: agent } = useAgents();
  const tick = useRefresh();

  useEffect(() => {
    api.aiConfig().then((c) => setAiOn(c.configured)).catch(() => setAiOn(false));
  }, []);
  useEffect(() => {
    api.sessionCards(range, agent).then((d) => setCards(d.sessions)).catch(() => setCards([]));
  }, [range, agent, tick]);

  const setSort = (s: SideSort) => {
    setSortState(s);
    try { localStorage.setItem(SORT_KEY, s); } catch { /* storage unavailable */ }
  };
  const setOpen = (o: boolean) => {
    setOpenState(o);
    try { localStorage.setItem(RECENTS_KEY, o ? "1" : "0"); } catch { /* storage unavailable */ }
  };

  const groups = useMemo(() => recentGroups(cards ?? [], sort, currentSessionId), [cards, sort, currentSessionId]);
  const count = sessionCount ?? cards?.length;

  return (
    <aside className="side" aria-label="Main">
      <Link className="brand" to="/" aria-label="infrapal Token Advisor, Overview"><Logo /></Link>
      <nav className="nav" aria-label="Main">
        <NavLink to="/" end className={onCls}><span className="nav-l"><IconGrid size={18} />Overview</span></NavLink>
        <NavLink to="/sessions" className={onCls}>
          <span className="nav-l"><IconChat size={18} />Sessions</span>
          {count !== undefined && <span className="v badge">{count}</span>}
        </NavLink>
        <NavLink to="/fixes" className={onCls}>
          <span className="nav-l"><IconWrench size={18} />Fixes</span>
          {fixesToDo !== undefined && <span className="v badge amber">{fixesToDo} to do</span>}
        </NavLink>
      </nav>
      <div className="side-filters">
        <label htmlFor="side-range" className="sr">Time range</label>
        <select id="side-range" className="sel-dark" value={range} onChange={(e) => setRange(e.target.value as typeof range)}>
          {PERIODS.map((p) => <option key={p.value} value={p.value}>{p.short}</option>)}
        </select>
        <label htmlFor="side-sort" className="sr">Sort recent sessions</label>
        <select id="side-sort" className="sel-dark" value={sort} onChange={(e) => setSort(e.target.value as SideSort)}>
          {SIDE_SORTS.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
        </select>
      </div>
      <AgentPicker />
      <section className="recents" aria-label="Recent sessions">
        <button type="button" className={`recents-head${open ? " open" : ""}`} aria-expanded={open} onClick={() => setOpen(!open)}>
          Recents<IconChevronDown size={16} />
        </button>
        {open && (
          <div className="recents-list">
            {cards !== null && groups.length === 0 && <p className="recents-empty">No sessions in this range.</p>}
            {groups.map((g) => (
              <div className="rgroup" key={g.project}>
                <div className="rgroup-head"><span><IconFolder size={16} />{g.project}</span><span className="mono">{g.total}</span></div>
                {g.rows.map((c) => (
                  <Link key={c.id} className={`rs${c.id === currentSessionId ? " on" : ""}${c.priority === "fine" ? " quiet" : ""}`} to={`/sessions/${encodeURIComponent(c.id)}`} aria-current={c.id === currentSessionId ? "page" : undefined}>
                    <span className={`dot ${c.priority}`} aria-hidden="true" />
                    <span className="name">{cleanTitle(c.title)}</span>
                    <span className="cost">{c.cost_unknown ? "—" : money(c.cost_usd)}</span>
                  </Link>
                ))}
              </div>
            ))}
          </div>
        )}
      </section>
      <div className="side-foot">
        <span className="local" title={`Prompt text: ${status?.prompts_logged_7d ? "logged" : "not logged"}`}>
          <span className={`pip ${status?.collector_live ? "full" : ""}`} />
          {status === null ? "Checking collector…" : status.collector_live ? "Local only · collector live" : "Local only · collector idle"}
        </span>
        <div className="foot-row">
          <NavLink className="foot-link" to="/settings"><IconSettings size={14} />Settings</NavLink>
          <button type="button" className="theme-toggle" onClick={toggleTheme} aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}>
            {theme === "dark" ? <><IconSun size={14} />Light</> : <><IconMoon size={14} />Dark</>}
          </button>
        </div>
        <div className="foot-meta">
          <span>Prompt text: {status?.prompts_logged_7d ? "logged" : "not logged"}</span>
          <NavLink to="/settings">{aiOn ? "AI insights · On" : "AI insights · Off — add key"}</NavLink>
        </div>
      </div>
    </aside>
  );
}

// onCls gives the current page's nav link the "on" class the styles use.
function onCls({ isActive }: { isActive: boolean }): string {
  return isActive ? "on" : "";
}

type RecentGroup = { project: string; total: number; rows: SessionCard[] };

const SIDE_SORTERS: Record<SideSort, (a: SessionCard, b: SessionCard) => number> = {
  priority: (a, b) => RANK[a.priority] - RANK[b.priority] || b.started_at - a.started_at,
  newest: (a, b) => b.started_at - a.started_at,
  cost: (a, b) => b.cost_usd - a.cost_usd,
};

// recentGroups groups sessions by project in the chosen order and keeps the
// list short. The open session is always kept, so it can be highlighted.
function recentGroups(cards: SessionCard[], sort: SideSort, currentId?: string): RecentGroup[] {
  const sorted = [...cards].sort(SIDE_SORTERS[sort]);
  const order: string[] = [];
  const by: Record<string, SessionCard[]> = {};
  for (const c of sorted) {
    if (!by[c.project]) {
      by[c.project] = [];
      order.push(c.project);
    }
    by[c.project].push(c);
  }
  const current = currentId ? cards.find((c) => c.id === currentId) : undefined;
  let projects = order.slice(0, MAX_PROJECTS);
  if (current && !projects.includes(current.project)) projects = [current.project, ...projects.slice(0, MAX_PROJECTS - 1)];
  return projects.map((p) => {
    let rows = by[p].slice(0, MAX_PER_PROJECT);
    if (current && current.project === p && !rows.includes(current)) rows = [current, ...rows.slice(0, MAX_PER_PROJECT - 1)];
    return { project: p, total: by[p].length, rows };
  });
}
