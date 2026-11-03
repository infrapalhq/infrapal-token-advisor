import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
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
    ["/no-such-page", "/"],
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
});
