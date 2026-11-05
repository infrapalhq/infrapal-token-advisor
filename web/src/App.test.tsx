import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { fakeAPI } from "./test/api";
import { base, cards, fixes, overview, progress, timeline } from "./test/fixtures";
import { renderAt } from "./test/render";

const routes = () => ({
  ...base(),
  "/api/overview": overview(),
  "/api/sessions/cards": cards([]),
  "/api/sessions/s%201/timeline": timeline(),
  "/api/sessions/s%201/issues": { session_id: "s 1", areas: {} },
  "/api/fixes": fixes(),
  "/api/progress": progress(),
  "/api/mcp/setup": {},
  "/api/ai/recommendations": {},
});

describe("old links", () => {
  it.each([
    ["/spend?range=30d", "/?range=30d"],
    ["/waste", "/"],
    ["/optimize", "/"],
    ["/tools", "/"],
    ["/search?q=x", "/sessions?q=x"],
    ["/ai-mode", "/settings"],
    ["/trace", "/sessions"],
    ["/waterfall?session=s%201", "/sessions/s%201"],
    ["/trace?session=s%201", "/sessions/s%201"],
  ])("%s goes to %s", async (from, to) => {
    fakeAPI(routes());
    renderAt(from);
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe(to));
  });
});

describe("visits", () => {
  it("records a visit to Sessions, and not to other pages", async () => {
    const calls = fakeAPI(routes());
    renderAt("/");
    await screen.findByText("Fix first");
    expect(calls.some((c) => c.path === "/api/ui/visit")).toBe(false);

    const later = fakeAPI(routes());
    renderAt("/sessions");
    await waitFor(() => expect(later.some((c) => c.method === "POST" && c.path === "/api/ui/visit")).toBe(true));
  });

  it("shows a not-found page for an address with no page", async () => {
    fakeAPI(routes());
    renderAt("/no-such-page");
    expect(await screen.findByText("There is no page here")).toBeTruthy();
    expect(screen.getByTestId("where").textContent).toBe("/no-such-page");
  });

  it("shows a way back when a page crashes, not a blank screen", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    // An overview with no fix list makes the page throw while it renders.
    fakeAPI({ ...routes(), "/api/overview": { ...overview(), fix_first: null } });
    renderAt("/");
    expect(await screen.findByText("This page hit a problem")).toBeTruthy();
    expect(screen.getByRole("link", { name: "← Back to Overview" })).toBeTruthy();
  });
});
