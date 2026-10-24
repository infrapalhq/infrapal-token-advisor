import { useEffect, useState, type ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { api, type Status } from "./api";

// Shell is the sidebar layout shared by Overview, Sessions, Fixes and Settings.
export function Shell({ children, fixesToDo, sessionCount }: { children: ReactNode; fixesToDo?: number; sessionCount?: number }) {
  return (
    <div className="shell">
      <Sidebar fixesToDo={fixesToDo} sessionCount={sessionCount} />
      <main className="main">{children}</main>
    </div>
  );
}

export function Sidebar({ fixesToDo, sessionCount }: { fixesToDo?: number; sessionCount?: number }) {
  const status = useStatus();
  const [aiOn, setAiOn] = useState(false);
  useEffect(() => {
    api.aiConfig().then((c) => setAiOn(c.configured)).catch(() => setAiOn(false));
  }, []);
  return (
    <aside className="side" aria-label="Main">
      <div className="brand">
        <div className="brand-mark" aria-hidden="true" />
        <div>
          <div className="brand-name">infrapal</div>
          <div className="brand-sub">token advisor</div>
        </div>
      </div>
      <nav className="nav" aria-label="Main">
        <NavLink to="/" end>Overview</NavLink>
        <NavLink to="/sessions">
          Sessions{sessionCount !== undefined && <span className="v">{sessionCount}</span>}
        </NavLink>
        <NavLink to="/fixes">
          Fixes{fixesToDo !== undefined && <span className="v">{fixesToDo} to do</span>}
        </NavLink>
        <NavLink to="/settings">Settings</NavLink>
      </nav>
      <div className="side-foot">
        <span className="local">
          <span className={`pip ${status?.collector_live ? "full" : ""}`} />
          {status === null ? "Checking collector…" : status.collector_live ? "Local only · collector live" : "Local only · collector idle"}
        </span>
        <span>Prompt text: {status?.prompts_logged_7d ? "logged" : "not logged"}</span>
        <NavLink to="/settings">{aiOn ? "AI insights · On" : "AI insights · Off — add key"}</NavLink>
      </div>
    </aside>
  );
}

function useStatus(): Status | null {
  const [s, setS] = useState<Status | null>(null);
  useEffect(() => {
    let live = true;
    const load = () => api.status().then((v) => live && setS(v)).catch(() => {});
    load();
    const t = setInterval(load, 30_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, []);
  return s;
}
