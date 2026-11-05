import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { base, mcpSetup } from "../test/fixtures";
import { renderAt } from "../test/render";

describe("MCP setup", () => {
  it("shows how to turn it on, and checks again", async () => {
    let enabled = false;
    fakeAPI({ ...base(), "/api/mcp/setup": () => mcpSetup({ enabled }) });
    renderAt("/settings");
    const mcp = within(await screen.findByRole("region", { name: "Enable MCP" }));
    expect(mcp.getByText("Enable MCP to ask Claude Code about this data")).toBeTruthy();
    expect(mcp.getByText(/so there's no API key to add/)).toBeTruthy();
    expect(mcp.getByText("/infrapal-token-advisor:review")).toBeTruthy();
    expect(mcp.getByText("Off")).toBeTruthy();
    expect(mcp.getByText("claude mcp add infrapal-token-advisor")).toBeTruthy();
    expect(mcp.getByText("claude mcp list")).toBeTruthy();
    enabled = true;
    await userEvent.click(mcp.getByRole("button", { name: "Check again" }));
    expect(await mcp.findByText("MCP is enabled")).toBeTruthy();
    expect(mcp.getByText("claude mcp remove infrapal-token-advisor")).toBeTruthy();
    expect(mcp.queryByText("claude mcp list")).toBeNull();
  });

  it("copies a command", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    fakeAPI({ ...base(), "/api/mcp/setup": mcpSetup() });
    renderAt("/settings");
    const mcp = within(await screen.findByRole("region", { name: "Enable MCP" }));
    await userEvent.click(mcp.getAllByRole("button", { name: "Copy" })[0]);
    expect(writeText).toHaveBeenCalledWith("claude mcp add infrapal-token-advisor");
    expect(mcp.getByRole("button", { name: "Copied" })).toBeTruthy();
  });

  it("says why setup couldn't load", async () => {
    fakeAPI({ ...base(), "/api/mcp/setup": new HTTPError(500, "no binary path") });
    renderAt("/settings");
    expect(await screen.findByText("Could not load MCP setup: no binary path")).toBeTruthy();
  });

  it("switches between dark and light at the bottom of the page", async () => {
    fakeAPI(base());
    renderAt("/settings");
    await userEvent.click(await screen.findByRole("button", { name: "Light Mode" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    await userEvent.click(screen.getByRole("button", { name: "Dark Mode" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});
