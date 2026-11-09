import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { agent, base, cards, overview, status } from "../test/fixtures";
import { renderAt } from "../test/render";

describe("Overview", () => {
  describe.each([
    { cost_unknown_sessions: 0, headline: "$12.50 spent" },
    { cost_unknown_sessions: 3, headline: "1.5M tokens used" },
  ])("$headline", ({ cost_unknown_sessions, headline }) => {
    it.each([
      { range: "today", period: "today" },
      { range: "7d", period: "in the last 7 days" },
      { range: "30d", period: "in the last 30 days" },
      { range: "3m", period: "in the last 3 months" },
    ])("formats the $range headline", async ({ range, period }) => {
      fakeAPI({ ...base(), "/api/overview": overview({ cost_unknown_sessions }) });
      renderAt(`/?range=${range}`);
      expect(await screen.findByRole("heading", { name: `${headline} ${period}.` })).toBeTruthy();
    });
  });

  it("shows spend, what to fix first and where it went", async () => {
    fakeAPI({ ...base(), "/api/overview": overview() });
    renderAt("/");
    expect(await screen.findByRole("heading", { name: "$12.50 spent in the last 7 days." })).toBeTruthy();
    expect(screen.getByText(/2 things worth fixing/).textContent).toBe(
      "2 things worth fixing. Fixing the top three recovers about $2.10 and saves about 4 minutes of waiting.",
    );
    const kpis = within(screen.getByRole("region", { name: "Key numbers" }));
    expect(kpis.getByText("1.5M")).toBeTruthy();
    expect(kpis.getByText("62% served from cache")).toBeTruthy();
    expect(kpis.getByText("≈ $3.40")).toBeTruthy();
    expect(kpis.getByText("1m 15s")).toBeTruthy();
    expect(screen.getByText("Re-read src/a.ts 6 times")).toBeTruthy();
    expect(screen.getByText("≈ $1.25")).toBeTruthy();
    expect(screen.getByText("3m")).toBeTruthy();
    expect(screen.getByText("1 issue · repeat approvals and rejections")).toBeTruthy();
    expect(screen.getByText("Models: claude-opus-5-5 100%")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Fix the login bug/ }).getAttribute("href")).toBe("/sessions/s1");
    expect(screen.getByText("2 to do")).toBeTruthy();
  });

  it("says when there's nothing to fix", async () => {
    fakeAPI({ ...base(), "/api/overview": overview({ fix_first: [], open_fixes: 0, recovered: { usd: 0, minutes: 0 }, review: [] }) });
    renderAt("/");
    expect(await screen.findByText("Nothing to fix in this range.")).toBeTruthy();
    expect(screen.getByText(/things worth fixing/).textContent).toBe("0 things worth fixing.");
    expect(screen.queryByText("Sessions to review")).toBeNull();
  });

  it("shows tokens instead of spend when no session has cost data", async () => {
    fakeAPI({ ...base(), "/api/overview": overview({ cost_unknown_sessions: 3, open_fixes: 1, fix_first: [overview().fix_first[0]], recovered: { usd: 0, minutes: 1 } }) });
    renderAt("/");
    expect(await screen.findByRole("heading", { name: "1.5M tokens used in the last 7 days." })).toBeTruthy();
    expect(screen.getByText(/thing worth fixing/).textContent).toBe("1 thing worth fixing. Fixing the top three saves about 1 minute of waiting.");
    expect(screen.queryByText("Spend by day")).toBeNull();
  });

  it("says how many sessions the spend leaves out", async () => {
    fakeAPI({ ...base(), "/api/overview": overview({ cost_unknown_sessions: 1 }) });
    renderAt("/");
    expect(await screen.findByText("Spend leaves out 1 session with no cost data.")).toBeTruthy();
  });

  it("loads the chosen period", async () => {
    const calls = fakeAPI({ ...base(), "/api/overview": overview() });
    renderAt("/");
    await screen.findByText("Fix first");
    await userEvent.selectOptions(screen.getByLabelText("Period"), "30d");
    expect(await screen.findByRole("heading", { name: "$12.50 spent in the last 30 days." })).toBeTruthy();
    expect(calls.filter((c) => c.path.startsWith("/api/overview")).map((c) => c.path)).toEqual([
      "/api/overview?since=7d",
      "/api/overview?since=30d",
    ]);
  });

  it("shows why it couldn't load", async () => {
    fakeAPI({ ...base(), "/api/overview": new HTTPError(500, "database is locked") });
    renderAt("/");
    expect(await screen.findByText("Could not load: database is locked")).toBeTruthy();
  });

  it("explains how to start before anything has arrived", async () => {
    fakeAPI({ ...base(), "/api/status": status({ last_event_ms: undefined }), "/api/overview": overview() });
    renderAt("/");
    expect(await screen.findByRole("heading", { name: "Waiting for your first session." })).toBeTruthy();
    expect(screen.getByText("Connect Claude Code")).toBeTruthy();
  });

  it("filters by agent once there's more than one, and says what wasn't checked", async () => {
    const calls = fakeAPI({
      ...base(),
      "/api/agents": { agents: [agent(), agent({ id: "codex", name: "Codex", skipped: ["permission waits"] })] },
      "/api/overview": overview(),
    });
    renderAt("/");
    const picker = await screen.findByLabelText("Agent");
    expect(screen.getByText("Not checked for Codex, which doesn't report the data: permission waits.")).toBeTruthy();
    await userEvent.selectOptions(picker, "codex");
    await waitFor(() => expect(calls.some((c) => c.path === "/api/overview?since=7d&agent=codex")).toBe(true));
    expect(localStorage.getItem("ta-agent")).toBe("codex");
  });

  it("counts only session problems on a card, shows global fixes apart, and still opens Sessions", async () => {
    const o = overview({
      areas: [{ area: "context", items: 6, usd: 0, severity: "fine", sessions: 0, install_wide: 6 }],
      fix_first: [{ ...overview().fix_first[0], sessions: 0 }],
    });
    fakeAPI({ ...base(), "/api/overview": o });
    renderAt("/");
    const card = within(await screen.findByRole("link", { name: /Context health/ }));
    expect(card.getByText("0 open")).toBeTruthy();
    expect(card.getByText("0 issues · re-reads, compactions, mixed tasks")).toBeTruthy();
    expect(card.getByText("+ 6 global fixes, under Fixes")).toBeTruthy();
    expect(card.getByText("0 sessions →")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Context health/ }).getAttribute("href")).toBe("/sessions?area=context");
  });

  it("counts a card's own problems apart from a global one in the same area", async () => {
    const o = overview({ areas: [{ area: "context", items: 3, usd: 0, severity: "half", sessions: 2, install_wide: 1 }] });
    fakeAPI({ ...base(), "/api/overview": o });
    renderAt("/");
    const card = within(await screen.findByRole("link", { name: /Context health/ }));
    expect(card.getByText("2 open")).toBeTruthy();
    expect(card.getByText("2 issues · re-reads, compactions, mixed tasks")).toBeTruthy();
    expect(card.getByText("2 sessions →")).toBeTruthy();
    expect(card.getByText("+ 1 global fix, under Fixes")).toBeTruthy();
  });

  it("opens the Sessions list filtered by the problem", async () => {
    fakeAPI({ ...base(), "/api/overview": overview() });
    renderAt("/");
    expect((await screen.findByRole("link", { name: /Context health/ })).getAttribute("href")).toBe("/sessions?area=context");
  });

  it("leaves out models with no share", async () => {
    const o = overview({ models: [{ model: "big", cost_usd: 5, share: 1 }, { model: "tiny", cost_usd: 0, share: 0 }] });
    fakeAPI({ ...base(), "/api/overview": o });
    renderAt("/");
    expect(await screen.findByText("Models: big 100%")).toBeTruthy();
  });

  it("offers the same periods as Sessions, and remembers the choice across pages", async () => {
    const calls = fakeAPI({ ...base(), "/api/overview": overview(), "/api/sessions/cards": cards([]) });
    renderAt("/");
    const select = (await screen.findByLabelText("Period")) as HTMLSelectElement;
    expect([...select.options].map((o) => o.textContent)).toEqual(["Today", "Last 7 days", "Last 30 days", "Last 3 months"]);
    await userEvent.selectOptions(select, "3m");
    expect(await screen.findByRole("heading", { name: "$12.50 spent in the last 3 months." })).toBeTruthy();
    expect(calls.map((c) => c.path)).toContain("/api/overview?since=3m");
    await userEvent.click(screen.getByRole("link", { name: /Sessions/ }));
    expect(await screen.findByText(/in the last 3 months/)).toBeTruthy();
  });

  it("falls back to 7 days for a range it doesn't know, instead of an error", async () => {
    fakeAPI({ ...base(), "/api/overview": overview() });
    renderAt("/?range=abc");
    expect(await screen.findByRole("heading", { name: "$12.50 spent in the last 7 days." })).toBeTruthy();
    expect(screen.queryByText(/Could not load/)).toBeNull();
    expect((screen.getByLabelText("Period") as HTMLSelectElement).value).toBe("7d");
  });

  it("links a problem with nothing open to Sessions, with no count link", async () => {
    const o = overview({ areas: [{ area: "perm", items: 0, usd: 0, severity: "fine", sessions: 0, install_wide: 0 }] });
    fakeAPI({ ...base(), "/api/overview": o });
    renderAt("/");
    const link = await screen.findByRole("link", { name: /Permission friction/ });
    expect(link.getAttribute("href")).toBe("/sessions?area=perm");
    expect(link.textContent).not.toContain("→");
  });

  it("shows a bar per week past two weeks, and folds small projects into Other", async () => {
    const by_day = Array.from({ length: 21 }, (_, i) => ({ day: `2026-10-${String(i + 1).padStart(2, "0")}`, cost_usd: 1, avoidable_usd: 0.5 }));
    const by_project = Array.from({ length: 8 }, (_, i) => ({ project: `p${i}`, cost_usd: 2 ** (8 - i), sessions: 1 }));
    fakeAPI({ ...base(), "/api/overview": overview({ by_day, by_project }) });
    renderAt("/");
    await screen.findByText("Spend by day");
    const bars = screen.getByRole("img", { name: "Daily spend" }).children;
    expect(bars).toHaveLength(3);
    expect(bars[0].getAttribute("title")).toBe("Oct 1: $7.00");
    expect(screen.getByText("Other (3 projects)")).toBeTruthy();
    expect(screen.getByText("$14.00", { selector: ".proj-row .mono" })).toBeTruthy();
    expect(screen.queryByText("p5")).toBeNull();
  });

  it("thins the day labels when there are many bars", async () => {
    const by_day = Array.from({ length: 12 }, (_, i) => ({ day: `2026-10-${String(i + 1).padStart(2, "0")}`, cost_usd: 1, avoidable_usd: 0 }));
    fakeAPI({ ...base(), "/api/overview": overview({ by_day }) });
    renderAt("/");
    await screen.findByText("Spend by day");
    const labels = [...document.querySelectorAll(".axis-x span")].map((s) => s.textContent);
    expect(labels).toHaveLength(12);
    expect(labels.filter(Boolean)).toEqual(["Oct 1", "Oct 3", "Oct 5", "Oct 7", "Oct 9", "Oct 11"]);
  });
});

