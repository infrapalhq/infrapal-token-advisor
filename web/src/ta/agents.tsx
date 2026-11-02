import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { AgentContext, useAgents } from "./agentContext";
import { api, type AgentInfo } from "./api";

const STORAGE_KEY = "ta-agent";

function savedAgent(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? "";
  } catch {
    return "";
  }
}

export function AgentProvider({ children }: { children: ReactNode }) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [saved, setSaved] = useState(savedAgent);

  useEffect(() => {
    api.agents().then((r) => setAgents(r.agents)).catch(() => {});
  }, []);

  const select = useCallback((id: string) => {
    setSaved(id);
    try {
      localStorage.setItem(STORAGE_KEY, id);
    } catch {
      // The choice just won't survive a reload.
    }
  }, []);

  // With one agent there's nothing to choose, and a saved choice can outlive
  // the agent it named.
  const selected = agents.length > 1 && agents.some((a) => a.id === saved) ? saved : "";
  const value = useMemo(
    () => ({ agents, selected, select, defaultName: agents[0]?.name ?? "your coding agent" }),
    [agents, selected, select],
  );
  return <AgentContext.Provider value={value}>{children}</AgentContext.Provider>;
}

// AgentPicker only appears once there's more than one agent to choose from.
export function AgentPicker() {
  const { agents, selected, select } = useAgents();
  if (agents.length < 2) return null;
  return (
    <>
      <label htmlFor="agent" className="sr">Agent</label>
      <select id="agent" className="btn agent-picker" value={selected} onChange={(e) => select(e.target.value)}>
        <option value="">All agents</option>
        {agents.map((a) => (
          <option key={a.id} value={a.id}>{a.name}</option>
        ))}
      </select>
    </>
  );
}

// SkippedChecks says what wasn't looked at, so an empty list isn't mistaken for
// a clean bill of health.
export function SkippedChecks() {
  const { agents, selected } = useAgents();
  const shown = agents.filter((a) => a.skipped.length > 0 && a.sessions > 0 && (!selected || a.id === selected));
  if (shown.length === 0) return null;
  return (
    <>
      {shown.map((a) => (
        <p className="hint-line" key={a.id}>Not checked for {a.name}, which doesn't report the data: {a.skipped.join(", ")}.</p>
      ))}
    </>
  );
}
