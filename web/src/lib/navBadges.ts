import { useEffect, useState } from "react";
import { getView, type PermissionFriction, type McpInventory, type WastedSpend } from "./api";

export type NavBadges = {
  waste: number;
  tools: number;
};

// Waste badge: total rejected tool calls (all time) — the same number the
// Waste page's "Rejected tool calls" KPI shows. Tools badge: MCP servers
// that connected but never had a tool called, plus tools whose success rate
// is below 90% — the two conditions the Tools & MCP page already flags with
// a fault/warn tone.
export function useNavBadges(): NavBadges {
  const [badges, setBadges] = useState<NavBadges>({ waste: 0, tools: 0 });

  useEffect(() => {
    Promise.all([
      getView<WastedSpend>("wasted_spend", { limit: "1000" }),
      getView<McpInventory>("mcp_inventory", { limit: "1000" }),
      getView<PermissionFriction>("permission_friction", { limit: "1000" }),
    ])
      .then(([wasted, inventory, friction]) => {
        const waste = wasted.reduce((sum, r) => sum + r.rejected_tool_count, 0);
        const deadServers = inventory.filter((s) => s.tool_call_count === 0).length;
        const lowSuccessTools = friction.filter((r) => {
          const total = r.success_count + r.error_count;
          return total > 0 && r.success_count / total < 0.9;
        }).length;
        setBadges({ waste, tools: deadServers + lowSuccessTools });
      })
      .catch(() => setBadges({ waste: 0, tools: 0 }));
  }, []);

  return badges;
}
