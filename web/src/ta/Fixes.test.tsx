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

  it("labels a finding with no session as global, and says so under session health", async () => {
    fakeAPI({ ...routes(), "/api/fixes": fixes({ to_fix: [item({ sessions: 0, usd: undefined, title: "srv connected but never used" })] }) });
    renderAt("/fixes");
    expect(await screen.findByText("global · Measured")).toBeTruthy();
    expect(screen.getByText(/1 global fix below isn't included/)).toBeTruthy();
  });

  it("counts several global fixes, and shows minutes for one with no dollar figure", async () => {
    const a = item({ key: "dead_mcp|a", sessions: 0, usd: undefined, minutes: 3.4, title: "a connected but never used" });
    const b = item({ key: "dead_mcp|b", sessions: 0, usd: undefined, title: "b connected but never used" });
    fakeAPI({ ...routes(), "/api/fixes": fixes({ to_fix: [a, b] }) });
    renderAt("/fixes");
    expect(await screen.findByText(/2 global fixes below aren't included/)).toBeTruthy();
    expect(screen.getByText("3 min · global · Measured")).toBeTruthy();
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

describe("AI recommendations", () => {
  it("is not shown until a key is added in Settings", async () => {
    fakeAPI(routes());
    renderAt("/fixes");
    await screen.findByRole("heading", { name: /Avoidable spend/ });
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
