import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { agent, base, cards, overview, status } from "../test/fixtures";
import { renderAt } from "../test/render";

describe("Overview", () => {
  it("shows spend, what to fix first and where it went", async () => {
    fakeAPI({ ...base(), "/api/overview": overview() });
    renderAt("/");
    expect(await screen.findByRole("heading", { name: "$12.50 spent last 7 days." })).toBeTruthy();
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
    expect(screen.getByText("1 issue · 1 session")).toBeTruthy();
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
    expect(await screen.findByRole("heading", { name: "1.5M tokens used last 7 days." })).toBeTruthy();
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
    expect(await screen.findByRole("heading", { name: "$12.50 spent last 30 days." })).toBeTruthy();
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

  it("sends install-wide findings to Fixes instead of an empty session list", async () => {
    const o = overview({
      areas: [{ area: "context", items: 6, usd: 0, severity: "half", sessions: 0, install_wide: 6 }],
      fix_first: [{ ...overview().fix_first[0], sessions: 0 }],
    });
    fakeAPI({ ...base(), "/api/overview": o });
    renderAt("/");
    expect(await screen.findByText("6 issues · 6 install-wide")).toBeTruthy();
    expect(screen.queryByText(/0 sessions/)).toBeNull();
    expect(screen.getByRole("link", { name: /Context health/ }).getAttribute("href")).toBe("/fixes");
    expect(screen.getByText("See fixes →")).toBeTruthy();
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
    expect(await screen.findByRole("heading", { name: "$12.50 spent last 3 months." })).toBeTruthy();
    expect(calls.map((c) => c.path)).toContain("/api/overview?since=3m");
    await userEvent.click(screen.getByRole("link", { name: /Sessions/ }));
    expect(await screen.findByText(/in the last 3 months/)).toBeTruthy();
  });

  it("falls back to 7 days for a range it doesn't know, instead of an error", async () => {
    fakeAPI({ ...base(), "/api/overview": overview() });
    renderAt("/?range=abc");
    expect(await screen.findByRole("heading", { name: "$12.50 spent last 7 days." })).toBeTruthy();
    expect(screen.queryByText(/Could not load/)).toBeNull();
    expect((screen.getByLabelText("Period") as HTMLSelectElement).value).toBe("7d");
  });

  it("links a problem with nothing open to Sessions, with no 'see' link", async () => {
    const o = overview({ areas: [{ area: "perm", items: 0, usd: 0, severity: "fine", sessions: 0, install_wide: 0 }] });
    fakeAPI({ ...base(), "/api/overview": o });
    renderAt("/");
    const link = await screen.findByRole("link", { name: /Permission friction/ });
    expect(link.getAttribute("href")).toBe("/sessions");
    expect(link.textContent).not.toContain("→");
  });
});
