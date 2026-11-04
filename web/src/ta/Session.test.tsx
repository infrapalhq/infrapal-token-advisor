import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { base, card, cards, timeline } from "../test/fixtures";
import { renderAt } from "../test/render";
import type { Occurrence } from "./api";

const reread: Occurrence = {
  Kind: "reread",
  Area: "context",
  Subject: "src/a.ts",
  Evidence: ["t1", "gone"],
  Count: 2,
  Severity: "full",
  Conf: "Measured",
  Tokens: 20_000,
  USD: 0.75,
  Minutes: 1.25,
  Title: "Re-read src/a.ts",
  Detail: "Read twice.",
  Fix: "Read src/a.ts once.",
  FixWhere: "CLAUDE.md",
  Note: "Applies to every session.",
};
const waits: Occurrence = { ...reread, Kind: "wait", Area: "perm", Subject: "git", Evidence: ["t2"], Tokens: undefined, USD: undefined, Minutes: undefined, Title: "Waited on git status", Fix: undefined, Note: "", Detail: "Approve it once." };

const routes = (issues: Record<string, Occurrence[]> = { context: [reread], perm: [waits] }) => ({
  ...base(),
  "/api/sessions/s1/timeline": timeline(),
  "/api/sessions/s1/issues": { session_id: "s1", areas: issues },
  "/api/sessions/cards": cards([card(), card({ id: "s2", title: "Other session", priority: "fine", cost_unknown: true }), card({ id: "s3", title: "Third", priority: "look" })]),
});

describe("Session", () => {
  it("shows the session, its thread and its context chart", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    expect(await screen.findByRole("heading", { name: "Fix the login bug" })).toBeTruthy();
    expect(screen.getByText("$2.50", { selector: ".pill" })).toBeTruthy();
    expect(screen.getByText("31K tok")).toBeTruthy();
    expect(screen.getByText("Peaked ~40K · compacted 1×")).toBeTruthy();
    expect(screen.getByRole("button", { name: "src/a.ts · ~20K context" })).toBeTruthy();
    const thread = within(screen.getByRole("region", { name: "Session thread" }));
    expect(thread.getByText("Done.")).toBeTruthy();
    expect(thread.getByText("(prompt not logged)")).toBeTruthy();
    expect(thread.getByText("waited 12s")).toBeTruthy();
    expect(thread.getByText("Compacted")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Other session/ }).textContent).toContain("—");
  });

  it("shows each area's issues with their evidence and fix", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    expect(await screen.findByText("Re-read src/a.ts", { selector: "strong" })).toBeTruthy();
    expect(screen.getByText("≈ 20K tok · ≈ $0.75 · 1.3 min · Measured")).toBeTruthy();
    expect(screen.getByText("Fix · CLAUDE.md")).toBeTruthy();
    expect(screen.getByText("Applies to every session.")).toBeTruthy();
    expect(screen.getByRole("button", { name: /^Read src\/a.ts/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /^gone/ })).toBeTruthy();

    await userEvent.click(screen.getByRole("tab", { name: /Permissions/ }));
    expect(screen.getByText("no direct cost · Measured")).toBeTruthy();
    expect(screen.getByText("Habit")).toBeTruthy();
    expect(screen.getByText("Approve it once.")).toBeTruthy();

    await userEvent.click(screen.getByRole("tab", { name: /Model/ }));
    expect(screen.getByText("No model–task mismatch issues in this session")).toBeTruthy();
  });

  it("opens the first area that has issues", async () => {
    fakeAPI(routes({ perm: [waits] }));
    renderAt("/sessions/s1");
    expect(await screen.findByText("Waited on git status", { selector: "strong" })).toBeTruthy();
  });

  it("copies a fix and marks it fixed", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    const calls = fakeAPI({ ...routes(), "POST /api/fixes/reread%7Csrc%2Fa.ts/state": { key: "x", state: "fixed" } });
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith("Read src/a.ts once.");
    expect(screen.getByRole("button", { name: "Copied" })).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Mark as fixed" }));
    expect(await screen.findByText("Marked as fixed")).toBeTruthy();
    expect(screen.getByText("of 2 fixes marked").textContent).toBe("1 of 2 fixes marked");
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ state: "fixed" });
  });

  it("highlights a step from the chart, the evidence and the flag", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "git status · ~40K context" }));
    expect(document.getElementById("row-t2")?.className).toContain("hl");
    await userEvent.click(screen.getByRole("button", { name: /^Read src\/a.ts/ }));
    expect(document.getElementById("row-t1")?.className).toContain("hl");

    await userEvent.click(screen.getByRole("button", { name: "Receipt" }));
    await userEvent.click(screen.getByRole("button", { name: "Show issue in Permission friction" }));
    expect(screen.getByText("Waited on git status", { selector: "strong" })).toBeTruthy();
    expect(document.getElementById("row-t2")?.className).toContain("hl");
  });

  it("shows a receipt, and jumps to a prompt from it", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "Receipt" }));
    const receipt = within(screen.getByRole("article", { name: "Session receipt" }));
    expect(receipt.getByText("4 model calls")).toBeTruthy();
    expect(receipt.getByText("70%")).toBeTruthy();
    expect(receipt.getByRole("img", { name: "Token mix: 58% from cache, 32% fresh input, 6% output" })).toBeTruthy();
    expect(receipt.getByText("1 flag", { exact: false })).toBeTruthy();
    expect(receipt.getByText("(prompt not logged)")).toBeTruthy();
    expect(receipt.getByText("Context")).toBeTruthy();
    expect(receipt.queryByText("Permissions")).toBeNull();
    await userEvent.click(receipt.getByRole("button", { name: /Fix the login bug/ }));
    expect(document.getElementById("row-d1")?.className).toContain("hl");
    await userEvent.click(receipt.getByRole("button", { name: /prompt not logged/ }));
    expect(document.getElementById("row-d1")?.className).toContain("hl");
  });

  it("groups long sessions into fewer bars", async () => {
    const rows = Array.from({ length: 130 }, (_, i) => ({ id: `r${i}`, kind: "tool" as const, ts: i, name: "Read", text: `f${i}`, context_tokens: i * 100 }));
    fakeAPI({ ...routes({}), "/api/sessions/s1/timeline": timeline({ rows, compactions: 0, cost_unknown: true }) });
    renderAt("/sessions/s1");
    await screen.findByText("Peaked ~40K");
    const bars = within(screen.getByRole("group", { name: "Context size after each step" })).getAllByRole("button");
    expect(bars).toHaveLength(44);
    expect(bars[0].getAttribute("aria-label")).toBe("3 steps · ~200 context");
    expect(screen.getByText("—", { selector: ".pill" })).toBeTruthy();
  });

  it("shows the id when the session isn't in the list", async () => {
    fakeAPI({ ...routes(), "/api/sessions/cards": cards([]) });
    renderAt("/sessions/s1");
    expect(await screen.findByRole("heading", { name: "s1" })).toBeTruthy();
  });

  it("shows why it couldn't load", async () => {
    fakeAPI({ ...routes(), "/api/sessions/s1/timeline": new HTTPError(404, "session not found") });
    renderAt("/sessions/s1");
    expect(await screen.findByText("Session not found")).toBeTruthy();
    expect(screen.getByRole("link", { name: "← Back to Sessions" }).getAttribute("href")).toBe("/sessions");
  });

  it("switches theme from its own sidebar", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "Switch to light mode" }));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe("light"));
  });

  it("opens a session with nothing in it instead of going blank", async () => {
    // The API once sent null for these lists, and the page crashed on them.
    const empty = timeline({ rows: null as never, receipt: { ...timeline().receipt, prompts: null as never, by_area_usd: null as never } });
    fakeAPI({ ...routes(), "/api/sessions/s1/timeline": empty });
    renderAt("/sessions/s1");
    expect(await screen.findByRole("heading", { name: "Fix the login bug" })).toBeTruthy();
    expect(screen.getByText(/Nothing was recorded in this session/)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Receipt" }));
    expect(screen.getByText("SESSION RECEIPT")).toBeTruthy();
  });

  it("renders the assistant's markdown", async () => {
    const t = timeline();
    t.rows = [...t.rows, { id: "c2", kind: "claude", ts: 99, text: "**Done**\n- one\n- two" }];
    fakeAPI({ ...routes(), "/api/sessions/s1/timeline": t });
    renderAt("/sessions/s1");
    expect(await screen.findByText("Done", { selector: "strong" })).toBeTruthy();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
  });

  it("shows the error for any other failure", async () => {
    fakeAPI({ ...routes(), "/api/sessions/s1/timeline": new HTTPError(500, "database is locked") });
    renderAt("/sessions/s1");
    expect(await screen.findByText("Could not load this session")).toBeTruthy();
    expect(screen.getByText("database is locked")).toBeTruthy();
  });

  it("names an empty session that is not in the list", async () => {
    const empty = timeline({ rows: [] });
    fakeAPI({ ...routes(), "/api/sessions/cards": cards([]), "/api/sessions/s1/timeline": empty });
    renderAt("/sessions/s1");
    expect(await screen.findByRole("heading", { name: "Session with no activity" })).toBeTruthy();
  });

  it("lists the avoidable spend by problem, and leaves the section out when there is none", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "Receipt" }));
    expect(screen.getByText("AVOIDABLE, BY PROBLEM")).toBeTruthy();
    expect(screen.getByText("$0.75", { selector: ".rl .mono" })).toBeTruthy();
  });
});
