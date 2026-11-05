import { screen, within } from "@testing-library/react";
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
    expect(screen.getByText("peak ~40K · compacted once")).toBeTruthy();
    expect(screen.getByText("Unhealthy", { selector: ".ctx-top .hbadge" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "src/a.ts · ~20K context" })).toBeTruthy();
    const thread = within(screen.getByRole("region", { name: "Session thread" }));
    expect(thread.getByText("Done.")).toBeTruthy();
    expect(thread.getByText("(prompt not logged)")).toBeTruthy();
    expect(thread.getByText("12s")).toBeTruthy();
    expect(thread.getByText(/Waiting on you/)).toBeTruthy();
    expect(thread.getByText("Compacted")).toBeTruthy();
    expect((await screen.findByRole("link", { name: /Other session/ })).textContent).toContain("—");
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
    expect(receipt.getByText("70% well spent")).toBeTruthy();
    expect(receipt.getByRole("img", { name: "Token mix: 58% from cache, 32% fresh input, 6% output" })).toBeTruthy();
    expect(receipt.getByText("1 flag", { exact: false })).toBeTruthy();
    expect(receipt.getByText(/^\d+ · \(prompt not logged\)$/)).toBeTruthy();
    expect(receipt.getByText("Context health")).toBeTruthy();
    expect(receipt.queryByText("Permission friction")).toBeNull();
    await userEvent.click(receipt.getByRole("button", { name: /Fix the login bug/ }));
    expect(document.getElementById("row-d1")?.className).toContain("hl");
    await userEvent.click(receipt.getByRole("button", { name: /prompt not logged/ }));
    expect(document.getElementById("row-d1")?.className).toContain("hl");
  });

  it("groups long sessions into fewer bars", async () => {
    const rows = Array.from({ length: 130 }, (_, i) => ({ id: `r${i}`, kind: "tool" as const, ts: i, name: "Read", text: `f${i}`, context_tokens: i * 100 }));
    fakeAPI({ ...routes({}), "/api/sessions/s1/timeline": timeline({ rows, compactions: 0, cost_unknown: true }) });
    renderAt("/sessions/s1");
    await screen.findByText("peak ~40K");
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
    expect(screen.getAllByText("Claude", { selector: ".who b" }).length).toBeGreaterThan(0);
  });

  it("names a reply after the agent that wrote it", async () => {
    const t = timeline({ agent_name: "Codex" });
    t.rows = [...t.rows, { id: "c2", kind: "claude", ts: 99, text: "Done here." }];
    fakeAPI({ ...routes(), "/api/sessions/s1/timeline": t });
    renderAt("/sessions/s1");
    expect(await screen.findByText("Done here.")).toBeTruthy();
    expect(screen.getAllByText("Codex", { selector: ".who b" }).length).toBeGreaterThan(0);
    expect(screen.queryByText("Claude", { selector: ".who b" })).toBeNull();
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

  it("hides and shows the context chart", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "Hide" }));
    expect(screen.queryByRole("group", { name: "Context size after each step" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Show" }));
    expect(screen.getByRole("group", { name: "Context size after each step" })).toBeTruthy();
  });

  it("labels where each prompt starts on the chart, skipping ones too close to read", async () => {
    const rows = [
      { id: "d1", kind: "div" as const, ts: 1, text: "PROMPT 1", prompt_id: "p1" },
      ...Array.from({ length: 10 }, (_, i) => ({ id: `a${i}`, kind: "tool" as const, ts: 2 + i, name: "Read", text: `a${i}`, prompt_id: "p1", context_tokens: 1000 })),
      { id: "d2", kind: "div" as const, ts: 20, text: "PROMPT 2", prompt_id: "p2" },
      { id: "b0", kind: "tool" as const, ts: 21, name: "Read", text: "b0", prompt_id: "p2", context_tokens: 2000 },
      { id: "d3", kind: "div" as const, ts: 30, text: "PROMPT 3", prompt_id: "p3" },
      ...Array.from({ length: 10 }, (_, i) => ({ id: `c${i}`, kind: "tool" as const, ts: 31 + i, name: "Read", text: `c${i}`, prompt_id: "p3", context_tokens: 3000 })),
    ];
    fakeAPI({ ...routes({}), "/api/sessions/s1/timeline": timeline({ rows }) });
    renderAt("/sessions/s1");
    expect(await screen.findByText("PROMPT 1", { selector: ".prompt-axis span" })).toBeTruthy();
    expect(screen.getByText("PROMPT 2", { selector: ".prompt-axis span" })).toBeTruthy();
    // Prompt 3 starts one bar after prompt 2, too close for a second label.
    expect(screen.queryByText("PROMPT 3", { selector: ".prompt-axis span" })).toBeNull();
    expect(document.querySelectorAll(".bar.pstart")).toHaveLength(1);
  });

  it("collapses a long prompt until it is opened", async () => {
    const long = "word ".repeat(200).trim();
    const t = timeline();
    t.rows = [...t.rows, { id: "u3", kind: "you", ts: 99, text: long, prompt_id: "p3" }];
    fakeAPI({ ...routes(), "/api/sessions/s1/timeline": t });
    renderAt("/sessions/s1");
    const text = await screen.findByText(long);
    expect(text.className).toBe("clamp");
    await userEvent.click(screen.getByRole("button", { name: "Show more" }));
    expect(text.className).toBe("");
    await userEvent.click(screen.getByRole("button", { name: "Show less" }));
    expect(text.className).toBe("clamp");
  });

  it("rates each area's health, and the session's", async () => {
    fakeAPI(routes({ perm: [{ ...waits, Severity: "half" }] }));
    renderAt("/sessions/s1");
    expect(await screen.findByText("Worth a look", { selector: ".ctx-top .hbadge" })).toBeTruthy();
    expect(screen.getByText("Worth a look", { selector: ".ap-head .hbadge" })).toBeTruthy();
    await userEvent.click(screen.getByRole("tab", { name: /Model/ }));
    expect(screen.getByText("Healthy", { selector: ".ap-head .hbadge" })).toBeTruthy();
  });

  it("marks a flagged step with the finding's kind, and a flagged compaction with its confidence", async () => {
    const compacted: Occurrence = { ...reread, Kind: "compaction", Evidence: ["e1"], Conf: "Likely", Title: "Compacted once" };
    fakeAPI(routes({ context: [{ ...reread, Kind: "re_read" }, compacted] }));
    renderAt("/sessions/s1");
    const thread = within(await screen.findByRole("region", { name: "Session thread" }));
    expect(await thread.findByText("re-read")).toBeTruthy();
    expect(thread.getByText("Likely")).toBeTruthy();
  });

  it("names the agent on a turn that starts with tool calls", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    const thread = within(await screen.findByRole("region", { name: "Session thread" }));
    // Prompt 1 opens with Read, and its text reply comes later.
    const read = (await thread.findByText("Read")).closest(".trow") as HTMLElement;
    expect(within(read).getByText("Claude", { selector: ".who b" })).toBeTruthy();
    const bash = thread.getByText("Bash").closest(".trow") as HTMLElement;
    expect(within(bash).queryByText("Claude", { selector: ".who b" })).toBeNull();
  });

  it("lists the costliest prompts first on the receipt, a few at a time", async () => {
    const t = timeline();
    t.receipt.prompts = Array.from({ length: 12 }, (_, i) => ({ prompt_id: `q${i}`, label: `Ask ${i + 1}`, cost_usd: i === 3 ? 9 : 1 + i / 100, tokens: 1000, flags: 0 }));
    fakeAPI({ ...routes(), "/api/sessions/s1/timeline": t });
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "Receipt" }));
    const receipt = within(screen.getByRole("article", { name: "Session receipt" }));
    const names = () => receipt.getAllByText(/^\d+ · Ask \d+$/).map((e) => e.textContent);
    expect(names()).toEqual(["4 · Ask 4", "12 · Ask 12", "11 · Ask 11", "10 · Ask 10", "9 · Ask 9"]);
    await userEvent.click(receipt.getByRole("button", { name: "Show 5 more of 7" }));
    expect(names()).toHaveLength(10);
    await userEvent.click(receipt.getByRole("button", { name: "Show 2 more" }));
    expect(names()).toHaveLength(12);
    await userEvent.click(receipt.getByRole("button", { name: "Show less" }));
    expect(names()).toHaveLength(5);
  });

  it("names the project, length and model on the receipt", async () => {
    fakeAPI(routes());
    renderAt("/sessions/s1");
    await userEvent.click(await screen.findByRole("button", { name: "Receipt" }));
    expect(screen.getByText(/^api · .* · 1h 05m · claude-opus-5-5$/)).toBeTruthy();
  });
});

