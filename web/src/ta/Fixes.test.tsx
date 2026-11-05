import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { T0, aiConfig, base, fixes, item, progress, status } from "../test/fixtures";
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
  "/api/ai/recommendations": {},
});

describe("Fixes", () => {
  it("lists what to fix, biggest first, with the fix open on the first card", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    expect(await screen.findByRole("heading", { name: "Fixes" })).toBeTruthy();
    expect(screen.getByText("1 to fix · 2 marked fixed · ordered by impact")).toBeTruthy();
    const card = (await screen.findByText("Re-read src/a.ts 6 times")).closest("article")!;
    expect(within(card).getByText("≈ $1.25")).toBeTruthy();
    expect(within(card).getByText("2 sessions")).toBeTruthy();
    expect(within(card).getByText("Read src/a.ts once.")).toBeTruthy();
    expect(within(card).getByText("Paste into CLAUDE.md")).toBeTruthy();
    expect(screen.getByRole("link", { name: "See your progress report →" }).getAttribute("href")).toBe("/fixes/progress");
    expect(screen.getByText("Long sessions re-send old context.", { exact: false })).toBeTruthy();
  });

  it("closes and opens a card", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    const toggle = await screen.findByRole("button", { name: /Re-read src\/a.ts 6 times/ });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    await userEvent.click(toggle);
    expect(screen.queryByText("Read src/a.ts once.")).toBeNull();
    await userEvent.click(toggle);
    expect(screen.getByText("Read src/a.ts once.")).toBeTruthy();
  });

  it("merges findings that share one fix into a single card", async () => {
    const fix = "## Working with files\n- Grep before reading large files.\n- Don't re-read a file already in context.";
    const a = item({ key: "reread|a.go", subject: "a.go", title: "a.go read 2× in one prompt", fix, usd: undefined, tokens: 1000 });
    const b = item({ key: "reread|b.md", subject: "b.md", title: "b.md read 2× in one prompt", fix, usd: undefined, tokens: 2000 });
    fakeAPI({ ...routes(), "/api/fixes": fixes({ to_fix: [a, b] }) });
    renderAt("/fixes");
    expect(await screen.findByText("Grep before reading large files")).toBeTruthy();
    expect(screen.queryByText("a.go read 2× in one prompt")).toBeNull();
    expect(screen.getByText("Seen with: a.go, b.md", { exact: false })).toBeTruthy();
    expect(screen.getByText("≈ 3K tok")).toBeTruthy();
    expect(screen.getByText("1 to fix · 2 marked fixed · ordered by impact")).toBeTruthy();
  });

  it("labels a finding with no session as global, and shows minutes for one with no dollar figure", async () => {
    fakeAPI({ ...routes(), "/api/fixes": fixes({ to_fix: [item({ sessions: 0, usd: undefined, minutes: 3.4, title: "srv connected but never used" })] }) });
    renderAt("/fixes");
    expect(await screen.findByText("global")).toBeTruthy();
    expect(screen.getByText("3 min")).toBeTruthy();
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
    await userEvent.click(screen.getByRole("tab", { name: /Marked fixed/ }));
    expect(screen.getByText("Measuring")).toBeTruthy();
    expect(screen.getByText("Measured", { selector: ".ok" })).toBeTruthy();
    await userEvent.click(screen.getAllByRole("button", { name: "Not fixed after all? Move back" })[0]);
    await waitFor(() => expect(calls.filter((c) => c.method === "POST").map((c) => [c.path, c.body])).toEqual([
      ["/api/fixes/reread%7Csrc%2Fa.ts/state", { state: "fixed" }],
      ["/api/fixes/allow%7Cls/state", { state: "reverted" }],
    ]));
    // Each change reloads the page's data.
    await waitFor(() => expect(calls.filter((c) => c.path.startsWith("/api/fixes?")).length).toBe(3));
  });

  it("sets a fix aside as not relevant, and brings it back", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    await userEvent.click(await screen.findByRole("button", { name: "Not relevant" }));
    expect(screen.getByText("Nothing with a config fix right now.")).toBeTruthy();
    expect(localStorage.getItem("ta-fix-dismissed")).toBe(JSON.stringify(["reread|src/a.ts"]));
    await userEvent.click(screen.getByRole("tab", { name: /Not relevant/ }));
    await userEvent.click(screen.getByRole("button", { name: "Move back to To fix" }));
    await userEvent.click(screen.getByRole("tab", { name: /To fix/ }));
    expect(screen.getByText("Re-read src/a.ts 6 times")).toBeTruthy();
  });

  it("filters by problem area", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    await screen.findByText("Re-read src/a.ts 6 times");
    await userEvent.click(screen.getByRole("button", { name: "Permissions" }));
    expect(screen.queryByText("Re-read src/a.ts 6 times")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Context" }));
    expect(screen.getByText("Re-read src/a.ts 6 times")).toBeTruthy();
  });

  it("puts fixes with no measurable impact last, and shows a second figure under the first", async () => {
    const none = item({ key: "a|none", title: "No impact yet", usd: undefined, score: 99, fix: "x" });
    const both = item({ key: "b|both", title: "Tokens and dollars", tokens: 4000, usd: 0.5, score: 1, fix: "y" });
    const wait = item({ key: "c|wait", area: "perm", title: "Dollars and waiting", usd: 0.2, minutes: 4, score: 2, fix: "z" });
    fakeAPI({ ...routes(), "/api/fixes": fixes({ to_fix: [none, both, wait] }) });
    renderAt("/fixes");
    await screen.findByText("No impact yet");
    const titles = [...document.querySelectorAll(".fx-t strong")].map((e) => e.textContent);
    expect(titles).toEqual(["Dollars and waiting", "Tokens and dollars", "No impact yet"]);
    expect(screen.getByText("≈ 4K tok")).toBeTruthy();
    expect(screen.getByText("≈ $0.50")).toBeTruthy();
    expect(screen.getByText("4 min waiting")).toBeTruthy();
  });

  it("folds habits with the same advice together, and shows the rest on request", async () => {
    const habits = [
      item({ key: "h|1", title: "Bash(curl:*) approved by hand 2×", fix: undefined, note: "Not read-only.", score: 5 }),
      item({ key: "h|2", title: "Bash(env:*) approved by hand 2×", fix: undefined, note: "Not read-only.", score: 4 }),
      item({ key: "h|3", title: "Start fresh sessions", fix: undefined, note: "Long sessions cost more.", score: 3 }),
      item({ key: "h|4", title: "Split tasks", fix: undefined, note: "Mixed tasks re-send context.", score: 2 }),
      item({ key: "h|5", title: "Check MCP credentials", fix: undefined, note: "Calls failed.", score: 1 }),
    ];
    fakeAPI({ ...routes(), "/api/fixes": fixes({ habits }) });
    renderAt("/fixes");
    expect(await screen.findByText("Bash(curl:*) approved by hand 2×, and 1 more like it.")).toBeTruthy();
    expect(screen.queryByText("Check MCP credentials.")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Show all 4" }));
    expect(screen.getByText("Check MCP credentials.")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Show fewer" }));
    expect(screen.queryByText("Check MCP credentials.")).toBeNull();
  });

  it("ignores a broken not-relevant list in storage", async () => {
    localStorage.setItem("ta-fix-dismissed", "{nope");
    fakeAPI(routes());
    renderAt("/fixes");
    expect(await screen.findByText("Re-read src/a.ts 6 times")).toBeTruthy();
    localStorage.setItem("ta-fix-dismissed", JSON.stringify({ a: 1 }));
    renderAt("/fixes");
    expect((await screen.findAllByText("Re-read src/a.ts 6 times")).length).toBeGreaterThan(0);
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
    await userEvent.click(screen.getByRole("tab", { name: /Marked fixed/ }));
    expect(screen.getByText("Nothing marked yet. Mark a fix once you have applied it.")).toBeTruthy();
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

describe("Progress report", () => {
  it("shows how avoidable spend and healthy sessions moved", async () => {
    fakeAPI(routes());
    renderAt("/fixes/progress");
    expect(await screen.findByRole("heading", { name: "Avoidable spend is down 60% since you started." })).toBeTruthy();
    const panel = within(screen.getByRole("region", { name: "Your progress" }));
    expect(panel.getByText("80%")).toBeTruthy();
    expect(panel.getByText("$4.00")).toBeTruthy();
    expect(panel.getByText("was $10.00")).toBeTruthy();
    expect(panel.getByText("Oct 12 · 2 marked")).toBeTruthy();
  });

  it("says under session health when global fixes are left out", async () => {
    const a = item({ key: "dead_mcp|a", sessions: 0, usd: undefined, title: "a connected but never used" });
    const b = item({ key: "dead_mcp|b", sessions: 0, usd: undefined, title: "b connected but never used" });
    fakeAPI({ ...routes(), "/api/fixes": fixes({ to_fix: [a, b] }) });
    renderAt("/fixes/progress");
    expect(await screen.findByText(/2 global fixes below aren't included/)).toBeTruthy();
  });

  it("has no figures before the first week", async () => {
    fakeAPI({ ...routes(), "/api/progress": progress([]) });
    renderAt("/fixes/progress");
    expect(await screen.findByRole("heading", { name: "Avoidable spend since you started." })).toBeTruthy();
    expect(screen.getAllByText("—")).toHaveLength(2);
  });
});

describe("AI recommendations", () => {
  it("is not shown until a key is added in Settings", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    await screen.findByText("Re-read src/a.ts 6 times");
    expect(screen.queryByRole("region", { name: "AI recommendations" })).toBeNull();
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
