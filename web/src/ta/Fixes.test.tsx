import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { T0, aiConfig, base, fixes, mcpSetup, progress, status } from "../test/fixtures";
import { renderAt } from "../test/render";
import type { AiResult } from "./api";

const rec: AiResult = {
  batch_id: "b1",
  generated_at: T0,
  dropped_count: 1,
  recommendations: [
    { category: "context", severity: "warn", title: "Split long sessions", detail: "They re-send old context.", evidence: [{ label: "sessions", value: "4" }] },
    { category: "perm", severity: "info", title: "Allow git status", detail: "It's read-only.", evidence: [] },
  ],
};

const routes = () => ({
  ...base(),
  "/api/fixes": fixes(),
  "/api/progress": progress(),
  "/api/mcp/setup": mcpSetup(),
  "/api/ai/recommendations": {},
});

describe("Fixes", () => {
  it("shows progress, fixes, marked fixes and habits", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    expect(await screen.findByRole("heading", { name: "Avoidable spend is down 60% since you started." })).toBeTruthy();
    const progressPanel = within(screen.getByRole("region", { name: "Your progress" }));
    expect(progressPanel.getByText("80%")).toBeTruthy();
    expect(progressPanel.getByText("$4.00")).toBeTruthy();
    expect(progressPanel.getByText("was $10.00")).toBeTruthy();
    expect(progressPanel.getByText("Oct 12 · 2 marked")).toBeTruthy();
    expect(screen.getByText("Read src/a.ts once.")).toBeTruthy();
    expect(screen.getByText("≈ $1.25 · 2 sessions · Measured")).toBeTruthy();
    expect(screen.getByText("Measuring")).toBeTruthy();
    expect(screen.getByText("Measured", { selector: ".ok" })).toBeTruthy();
    expect(screen.getByText("Long sessions re-send old context.")).toBeTruthy();
    expect(screen.getByText("1 item fixed by how you work, not by a setting")).toBeTruthy();
  });

  it("copies a fix, marks it fixed and moves one back", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    const calls = fakeAPI({ ...routes(), "POST /api/fixes/reread%7Csrc%2Fa.ts/state": {}, "POST /api/fixes/allow%7Cls/state": {} });
    renderAt("/fixes");
    const card = (await screen.findByText("Read src/a.ts once.")).closest("article")!;
    await userEvent.click(within(card).getByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith("Read src/a.ts once.");
    expect(within(card).getByRole("button", { name: "Copied" })).toBeTruthy();
    await userEvent.click(within(card).getByRole("button", { name: "Mark as fixed" }));
    await userEvent.click(screen.getAllByRole("button", { name: "Not fixed after all? Move back" })[0]);
    await waitFor(() => expect(calls.filter((c) => c.method === "POST").map((c) => [c.path, c.body])).toEqual([
      ["/api/fixes/reread%7Csrc%2Fa.ts/state", { state: "fixed" }],
      ["/api/fixes/allow%7Cls/state", { state: "reverted" }],
    ]));
    // Each change reloads the page's data.
    await waitFor(() => expect(calls.filter((c) => c.path.startsWith("/api/fixes?")).length).toBe(3));
  });

  it("shows why a fix couldn't be marked", async () => {
    fakeAPI({ ...routes(), "POST /api/fixes/reread%7Csrc%2Fa.ts/state": new HTTPError(500, "database is locked") });
    renderAt("/fixes");
    await userEvent.click(await screen.findByRole("button", { name: "Mark as fixed" }));
    expect(await screen.findByText("Could not load: database is locked")).toBeTruthy();
  });

  it("says when there's nothing yet", async () => {
    fakeAPI({ ...routes(), "/api/fixes": fixes({ to_fix: [], marked: [], habits: [] }), "/api/progress": progress([]) });
    renderAt("/fixes");
    expect(await screen.findByText("Nothing with a config fix right now.")).toBeTruthy();
    expect(screen.getByText("Nothing marked yet. Mark a fix once you have applied it.")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Avoidable spend since you started." })).toBeTruthy();
    expect(screen.getAllByText("—")).toHaveLength(2);
  });

  it("shows why it couldn't load", async () => {
    fakeAPI({ ...routes(), "/api/progress": new HTTPError(500, "database is locked") });
    renderAt("/fixes");
    expect(await screen.findByText("Could not load: database is locked")).toBeTruthy();
  });

  it("explains how to start before anything has arrived", async () => {
    fakeAPI({ ...routes(), "/api/status": status({ last_event_ms: undefined }) });
    renderAt("/fixes");
    expect(await screen.findByRole("heading", { name: "Waiting for your first session." })).toBeTruthy();
  });
});

describe("MCP setup", () => {
  it("shows how to turn it on, and checks again", async () => {
    let enabled = false;
    fakeAPI({ ...routes(), "/api/mcp/setup": () => mcpSetup({ enabled }) });
    renderAt("/fixes");
    const mcp = within(await screen.findByRole("region", { name: "Enable MCP" }));
    expect(mcp.getByText("Enable MCP to ask Claude Code about this data")).toBeTruthy();
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
    fakeAPI(routes());
    renderAt("/fixes");
    const mcp = within(await screen.findByRole("region", { name: "Enable MCP" }));
    await userEvent.click(mcp.getAllByRole("button", { name: "Copy" })[0]);
    expect(writeText).toHaveBeenCalledWith("claude mcp add infrapal-token-advisor");
    expect(mcp.getByRole("button", { name: "Copied" })).toBeTruthy();
  });

  it("says why setup couldn't load", async () => {
    fakeAPI({ ...routes(), "/api/mcp/setup": new HTTPError(500, "no binary path") });
    renderAt("/fixes");
    expect(await screen.findByText("Could not load MCP setup: no binary path")).toBeTruthy();
  });
});

describe("AI recommendations", () => {
  it("points to Settings when there's no key", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    const ai = within(await screen.findByRole("region", { name: "AI recommendations" }));
    expect(ai.getByText("Off")).toBeTruthy();
    expect(ai.getByRole("link", { name: "Add a key in Settings" }).getAttribute("href")).toBe("/settings");
  });

  it("shows saved results, and generates again on request", async () => {
    const calls = fakeAPI({ ...routes(), "/api/ai/config": aiConfig({ configured: true }), "/api/ai/recommendations": rec, "POST /api/ai/recommendations": { ...rec, dropped_count: 0, recommendations: [] } });
    renderAt("/fixes");
    const ai = within(await screen.findByRole("region", { name: "AI recommendations" }));
    expect(await ai.findByText("Split long sessions")).toBeTruthy();
    expect(ai.getByText("sessions: 4")).toBeTruthy();
    expect(ai.getByText("Left out 1 recommendation that didn't match your data.")).toBeTruthy();
    expect(ai.getByText("Last generated Oct 20")).toBeTruthy();
    // Opening the page only reads saved results.
    expect(calls.some((c) => c.method === "POST")).toBe(false);
    await userEvent.click(ai.getByRole("button", { name: "Generate again" }));
    expect(await ai.findByText("Nothing to recommend yet. Try again after a few more sessions.")).toBeTruthy();
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ since: "30d", force: true });
  });

  it("generates for the first time, and shows a failure", async () => {
    const calls = fakeAPI({ ...routes(), "/api/ai/config": aiConfig({ configured: true }), "POST /api/ai/recommendations": new HTTPError(502, "the provider timed out") });
    renderAt("/fixes");
    const ai = within(await screen.findByRole("region", { name: "AI recommendations" }));
    await userEvent.click(await ai.findByRole("button", { name: "Generate" }));
    expect(await ai.findByRole("alert")).toBeTruthy();
    expect(ai.getByText("Could not generate: the provider timed out")).toBeTruthy();
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ since: "30d", force: false });
  });
});
