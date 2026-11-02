import { createContext, useContext } from "react";
import type { AgentInfo } from "./api";

export type Agents = {
  agents: AgentInfo[];
  // selected is the agent being viewed, or "" for all of them.
  selected: string;
  select: (id: string) => void;
  // defaultName names the agent new users are told to set up.
  defaultName: string;
};

export const AgentContext = createContext<Agents>({ agents: [], selected: "", select: () => {}, defaultName: "your coding agent" });

export function useAgents(): Agents {
  return useContext(AgentContext);
}
