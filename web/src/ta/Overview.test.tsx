import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { agent, base, overview, status } from "../test/fixtures";
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
    fakeAPI({ ...base(), "/api/overview": overview({ fix_first: [], recovered: { usd: 0, minutes: 0 }, review: [] }) });
    renderAt("/");
    expect(await screen.findByText("Nothing to fix in this range.")).toBeTruthy();
    expect(screen.getByText(/things worth fixing/).textContent).toBe("0 things worth fixing.");
    expect(screen.queryByText("Sessions to review")).toBeNull();
  });

  it("shows tokens instead of spend when no session has cost data", async () => {
    fakeAPI({ ...base(), "/api/overview": overview({ cost_unknown_sessions: 3, fix_first: [overview().fix_first[0]], recovered: { usd: 0, minutes: 1 } }) });
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
});
