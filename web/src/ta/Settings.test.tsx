import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { aiConfig, base, mcpSetup } from "../test/fixtures";
import { renderAt } from "../test/render";

describe("Settings", () => {
  it("saves a key without showing it again", async () => {
    const calls = fakeAPI({ ...base(), "PUT /api/ai/config": aiConfig({ configured: true }) });
    renderAt("/settings");
    expect(await screen.findByText("not set")).toBeTruthy();
    const input = screen.getByLabelText("Key");
    expect(screen.getByRole("button", { name: "Save key" }).hasAttribute("disabled")).toBe(true);
    await userEvent.type(input, "  sk-ant-test  ");
    await userEvent.click(screen.getByRole("button", { name: "Save key" }));
    expect(await screen.findByRole("status")).toBeTruthy();
    expect(screen.getByText("Key saved. It stays on this machine.")).toBeTruthy();
    expect(screen.getByText("configured")).toBeTruthy();
    expect((input as HTMLInputElement).value).toBe("");
    expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ api_key: "sk-ant-test" });
  });

  it("removes a key", async () => {
    const calls = fakeAPI({ ...base(), "/api/ai/config": aiConfig({ configured: true }), "PUT /api/ai/config": aiConfig() });
    renderAt("/settings");
    await userEvent.click(await screen.findByRole("button", { name: "Remove key" }));
    expect(await screen.findByText("Key removed.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Remove key" })).toBeNull();
    expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ api_key: "" });
  });

  it("shows why a key wasn't saved", async () => {
    fakeAPI({ ...base(), "PUT /api/ai/config": new HTTPError(400, "that doesn't look like an Anthropic key") });
    renderAt("/settings");
    await userEvent.type(await screen.findByLabelText("Key"), "nope");
    await userEvent.click(screen.getByRole("button", { name: "Save key" }));
    expect(await screen.findByText("that doesn't look like an Anthropic key")).toBeTruthy();
  });

  it("chooses a provider when there's more than one", async () => {
    const two = [{ id: "anthropic", name: "Anthropic" }, { id: "openai", name: "OpenAI" }];
    const calls = fakeAPI({
      ...base(),
      "/api/ai/config": aiConfig({ providers: two }),
      "PUT /api/ai/config": (body: unknown) => ((body as { provider: string }).provider === "openai" ? aiConfig({ providers: two, provider: "openai", provider_name: "OpenAI" }) : new HTTPError(400, "unknown provider")),
    });
    renderAt("/settings");
    await userEvent.selectOptions(await screen.findByLabelText("Provider"), "openai");
    expect(await screen.findByText("OpenAI API key")).toBeTruthy();
    expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ provider: "openai" });
    await userEvent.selectOptions(screen.getByLabelText("Provider"), "anthropic");
    expect(await screen.findByText("unknown provider")).toBeTruthy();
  });

  it("is still checking until the config loads", async () => {
    fakeAPI({ ...base(), "/api/ai/config": new HTTPError(500, "x") });
    renderAt("/settings");
    expect(await screen.findByText("checking…")).toBeTruthy();
    expect(screen.getByText("Provider API key")).toBeTruthy();
  });
});

describe("MCP setup", () => {
  it("shows how to turn it on, and checks again", async () => {
    let enabled = false;
    fakeAPI({ ...base(), "/api/mcp/setup": () => mcpSetup({ enabled }) });
    renderAt("/settings");
    const mcp = within(await screen.findByRole("region", { name: "Enable MCP" }));
    expect(mcp.getByText("Enable MCP to ask Claude Code about this data")).toBeTruthy();
    expect(mcp.getByText("Connect Infrapal Token Advisor to Claude Code to get AI powered insights")).toBeTruthy();
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
    await userEvent.click(await screen.findByRole("button", { name: "Switch to light mode" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(screen.getByText(/Light theme/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Switch to dark mode" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});
