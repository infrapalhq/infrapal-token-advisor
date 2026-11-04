import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { PERIODS, useRange } from "./range";
import { useAgents } from "./agentContext";
import { api, type SessionCard, type SessionCards } from "./api";
import { FirstRun } from "./FirstRun";
import { Shell } from "./Shell";
import { useFirstRun } from "./status";
import { useRefresh } from "./refresh";
import { AREA_LABEL, SYMPTOM_LABEL, dayShort, durationS, money, pipClass, tokensHuman, waitMs } from "./format";

const VIEWS = [
  { value: "all", label: "All" },
  { value: "attn", label: "Needs attention" },
  { value: "fresh", label: "New" },
] as const;
const SORTS = [
  { value: "priority", label: "Priority" },
  { value: "newest", label: "Newest" },
  { value: "cost", label: "Cost" },
  { value: "waiting", label: "Waiting on you" },
] as const;
const PRIORITY_RANK: Record<string, number> = { now: 0, look: 1, fine: 2 };
const PRIORITY_LABEL: Record<string, string> = { now: "FIX NOW", look: "WORTH A LOOK", fine: "FINE" };
const TIME_LABEL: Record<string, string> = {
  today: "today",
  "7d": "in the last 7 days",
  "30d": "in the last 30 days",
  "3m": "in the last 3 months",
};

type Sort = (typeof SORTS)[number]["value"];
type View = (typeof VIEWS)[number]["value"];

export default function Sessions() {
  const [params] = useSearchParams();
  const [range, setRange] = useRange();
  // loaded remembers what its data was loaded for, so a change of range or
  // agent shows "Loading…" until the new data arrives.
  const [loaded, setLoaded] = useState<{ key: string; data: SessionCards } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [view, setView] = useState<View>("all");
  const [sort, setSort] = useState<Sort>("priority");
  const [group, setGroup] = useState<"project" | "none">("project");
  const [query, setQuery] = useState("");
  // ?area= comes from the Overview's problem cards.
  const [areaFilter, setAreaFilter] = useState<string[]>(() => {
    const a = params.get("area");
    return a && a in AREA_LABEL ? [a] : [];
  });
  const [symFilter, setSymFilter] = useState<string[]>([]);
  const [popOpen, setPopOpen] = useState(false);
  const firstRun = useFirstRun();
  const { selected: agent } = useAgents();

  const key = `${range}|${agent}|${firstRun}`;
  const tick = useRefresh();
  useEffect(() => {
    api.sessionCards(range, agent).then((d) => {
      setLoaded({ key, data: d });
      setError(null);
    }).catch((e: Error) => setError(e.message));
  }, [range, agent, key, tick]);
  const data = loaded?.key === key ? loaded.data : null;

  const visible = useMemo(() => {
    if (!data) return [];
    const q = query.trim().toLowerCase();
    const list = data.sessions
      .filter((s) => !q || `${s.title} ${s.project}`.toLowerCase().includes(q))
      .filter((s) => areaFilter.length === 0 || s.areas.some((a) => areaFilter.includes(a.area)))
      .filter((s) => symFilter.every((k) => s.symptoms.includes(k as SessionCard["symptoms"][number])))
      .filter((s) => view === "all" || (view === "attn" ? s.priority !== "fine" : s.is_new));
    return [...list].sort(SORTERS[sort]);
  }, [data, query, areaFilter, symFilter, view, sort]);

  const groups = useMemo(() => {
    if (group === "none") return [{ name: "", rows: visible }];
    const order: string[] = [];
    const byProject: Record<string, SessionCard[]> = {};
    for (const s of visible) {
      if (!byProject[s.project]) {
        byProject[s.project] = [];
        order.push(s.project);
      }
      byProject[s.project].push(s);
    }
    return order.map((name) => ({ name, rows: byProject[name] }));
  }, [visible, group]);

  const activeFilters = areaFilter.length + symFilter.length;
  const setTime = setRange;
  const filtered = activeFilters > 0 || query.trim() !== "" || view !== "all";
  const installWide = data?.count_install_wide ?? 0;

  if (firstRun) return <Shell><FirstRun /></Shell>;

  return (
    <Shell sessionCount={data?.count_all}>
      <div className="page-head">
        <div className="title-block">
          <h1 className="title">Sessions</h1>
          <p className="sub">
            {data
              ? `${filtered ? `${visible.length} of ${data.count_all}` : data.count_all} sessions ${TIME_LABEL[range]} · ${data.count_attention} need attention · ${data.count_new} new since your last visit`
              : "Loading…"}
          </p>
        </div>
        <div className="search">
          <label htmlFor="q" className="sr">Search sessions</label>
          <input id="q" type="search" placeholder="Search sessions" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
      </div>

      <div className="toolbar">
        <div className="toolbar-l">
          <div className="seg-group" role="group" aria-label="Time range">
            {PERIODS.map((t) => (
              <button key={t.value} className={`seg${range === t.value ? " on" : ""}`} type="button" aria-pressed={range === t.value} onClick={() => setTime(t.value)}>
                {t.short}
              </button>
            ))}
          </div>
          <div className="vtabs" role="tablist" aria-label="View">
            {VIEWS.map((v) => (
              <button key={v.value} className={`vt${view === v.value ? " on" : ""}`} role="tab" type="button" aria-selected={view === v.value} onClick={() => setView(v.value)}>
                {v.label}
                <span className="cnt">{data ? (v.value === "all" ? data.count_all : v.value === "attn" ? data.count_attention : data.count_new) : "·"}</span>
              </button>
            ))}
          </div>
        </div>
        <div className="toolbar-r">
          <label className="lbl" htmlFor="srt">Sort</label>
          <select id="srt" className="select" value={sort} onChange={(e) => setSort(e.target.value as Sort)}>
            {SORTS.map((s) => (
              <option key={s.value} value={s.value}>{s.label}</option>
            ))}
          </select>
          <button className="btn" type="button" aria-expanded={popOpen} onClick={() => setPopOpen((o) => !o)}>
            Filters{activeFilters > 0 && <span className="cnt solid">{activeFilters}</span>}
          </button>
          {popOpen && (
            <div className="pop" role="dialog" aria-label="Filters">
              <div>
                <p className="ph">PROBLEM</p>
                {(["context", "exec", "perm", "model"] as const).map((k) => (
                  <label className="ck" key={k}>
                    <input type="checkbox" checked={areaFilter.includes(k)} onChange={(e) => setAreaFilter(toggle(areaFilter, k, e.target.checked))} />
                    {AREA_LABEL[k]}
                  </label>
                ))}
              </div>
              <div>
                <p className="ph">SYMPTOM</p>
                {(["compacted", "failed", "waits", "repeats"] as const).map((k) => (
                  <label className="ck" key={k}>
                    <input type="checkbox" checked={symFilter.includes(k)} onChange={(e) => setSymFilter(toggle(symFilter, k, e.target.checked))} />
                    {SYMPTOM_LABEL[k]}
                  </label>
                ))}
              </div>
              <div>
                <p className="ph">GROUP BY</p>
                <div className="seg-group">
                  {(["project", "none"] as const).map((g) => (
                    <button key={g} className={`seg grow${group === g ? " on" : ""}`} type="button" aria-pressed={group === g} onClick={() => setGroup(g)}>
                      {g === "project" ? "Project" : "None"}
                    </button>
                  ))}
                </div>
              </div>
              <div className="pop-foot">
                <button className="btn btn-q u" type="button" onClick={() => { setAreaFilter([]); setSymFilter([]); setView("all"); }}>Clear all</button>
                <button className="btn btn-k" type="button" onClick={() => setPopOpen(false)}>Done</button>
              </div>
            </div>
          )}
        </div>
      </div>

      {error && <p className="hint-line">Could not load: {error}</p>}
      {!data && !error && <p className="hint-line">Loading…</p>}

      {data && installWide > 0 && (
        <p className="hint-line install-note">
          {installWide} global {installWide === 1 ? "fix applies" : "fixes apply"} to your whole install, not one session (for example an unused MCP server), so no session below lists {installWide === 1 ? "it" : "them"}. <Link to="/fixes">See fixes →</Link>
        </p>
      )}

      {data && (
        <div id="results">
          {activeFilters > 0 && (
            <div className="chips">
              {areaFilter.map((k) => (
                <button key={k} className="chipx" type="button" onClick={() => setAreaFilter(areaFilter.filter((x) => x !== k))}>
                  {AREA_LABEL[k]}<span aria-hidden="true">✕</span>
                </button>
              ))}
              {symFilter.map((k) => (
                <button key={k} className="chipx" type="button" onClick={() => setSymFilter(symFilter.filter((x) => x !== k))}>
                  {SYMPTOM_LABEL[k]}<span aria-hidden="true">✕</span>
                </button>
              ))}
              <button className="btn btn-q u" type="button" onClick={() => { setAreaFilter([]); setSymFilter([]); }}>Clear all</button>
            </div>
          )}

          {visible.length === 0 ? (
            <div className="empty">
              <strong>No sessions match</strong>
              <span>
                {areaFilter.length > 0 && installWide > 0
                  ? "No session has this problem. Global fixes aren't tied to a session; they're listed under Fixes."
                  : "Try a longer time range, or clear your filters."}
              </span>
              <button className="btn" type="button" onClick={() => { setAreaFilter([]); setSymFilter([]); setView("all"); setQuery(""); }}>Clear filters</button>
            </div>
          ) : (
            <div className="table">
              <div className="table-inner">
                <div className="grid-row thead">
                  <span className="th">Priority</span><span className="th">Session</span><span className="th">Problems</span>
                  <span className="th r">Cost</span><span className="th r">Waiting</span><span />
                </div>
                {groups.map((g) => (
                  <div className="group" key={g.name || "all"}>
                    {g.name && (
                      <div className="group-head">
                        <span>{g.name}</span>
                        <span className="meta-r">{g.rows.length} session{g.rows.length === 1 ? "" : "s"} · {g.rows.every((s) => s.cost_unknown) ? "—" : money(g.rows.reduce((n, s) => n + s.cost_usd, 0))}</span>
                      </div>
                    )}
                    {g.rows.map((s) => (
                      <SessionRow key={s.id} s={s} showProject={!g.name} />
                    ))}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      <p className="foot-note">
        Priority = impact × confidence × how often it repeats. Ties go to the newest session. NEW = since your last visit.
      </p>
    </Shell>
  );
}

function SessionRow({ s, showProject }: { s: SessionCard; showProject: boolean }) {
  const label = PRIORITY_LABEL[s.priority];
  const meta = `${dayShort(s.started_at)} · ${durationS(s.duration_s)} · ${tokensHuman(s.tokens)} tok${showProject ? ` · ${s.project}` : ""}`;
  return (
    <Link className={`grid-row srow${s.priority === "fine" ? " fine" : ""}`} to={`/sessions/${encodeURIComponent(s.id)}`}>
      <span className={`pr ${s.priority}`}>{label}</span>
      <span className="title-cell">
        <strong>{s.title}</strong>
        {s.is_new && <span className="new">NEW</span>}
        <span className="meta-line">{meta}</span>
      </span>
      <span className="areas">
        {s.areas.length > 0 ? (
          s.areas.map((a) => (
            <span className="area" key={a.area}>
              <span className={`pip ${pipClass(a.severity)}`} />
              {AREA_LABEL[a.area]}
            </span>
          ))
        ) : (
          <span className="nothing">Nothing to fix</span>
        )}
      </span>
      <span className="cost-cell">{s.cost_unknown ? "—" : money(s.cost_usd)}</span>
      <span className="wait-cell">{waitMs(s.wait_ms)}</span>
      <span className="chev" aria-hidden="true">›</span>
    </Link>
  );
}

const SORTERS: Record<Sort, (a: SessionCard, b: SessionCard) => number> = {
  priority: (a, b) => PRIORITY_RANK[a.priority] - PRIORITY_RANK[b.priority] || b.started_at - a.started_at,
  newest: (a, b) => b.started_at - a.started_at,
  cost: (a, b) => b.cost_usd - a.cost_usd,
  waiting: (a, b) => b.wait_ms - a.wait_ms,
};

function toggle<T>(list: T[], item: T, on: boolean): T[] {
  return on ? [...list.filter((x) => x !== item), item] : list.filter((x) => x !== item);
}
