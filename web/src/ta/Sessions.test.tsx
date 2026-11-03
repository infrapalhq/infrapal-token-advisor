import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { DAY, T0, base, card, cards, status } from "../test/fixtures";
import { renderAt } from "../test/render";

const list = () =>
  cards([
    card(),
    card({ id: "s2", project: "web", title: "Add dark mode", started_at: T0 + DAY, cost_usd: 0.4, wait_ms: 0, priority: "fine", is_new: false, areas: [], symptoms: [] }),
    card({ id: "s3", project: "api", title: "Speed up tests", started_at: T0 - DAY, cost_usd: 9, wait_ms: 300_000, priority: "look", is_new: false, areas: [{ area: "perm", severity: "half" }], symptoms: ["waits", "failed"] }),
    card({ id: "s4", project: "docs", title: "Write the guide", priority: "fine", is_new: false, areas: [], symptoms: [], cost_unknown: true }),
  ]);

const titles = () => screen.getAllByRole("link").map((a) => a.querySelector("strong")?.textContent).filter(Boolean);

describe("Sessions", () => {
  it("lists sessions by project, most urgent first", async () => {
    fakeAPI({ ...base(), "/api/sessions/cards": list() });
    renderAt("/sessions");
    expect(await screen.findByText("4 sessions in the last 7 days · 2 need attention · 1 new since your last visit")).toBeTruthy();
    expect(titles()).toEqual(["Fix the login bug", "Speed up tests", "Add dark mode", "Write the guide"]);
    expect(screen.getByText("2 sessions · $11.50")).toBeTruthy();
    expect(screen.getByText("1 session · —")).toBeTruthy();
    expect(screen.getByText("NEW")).toBeTruthy();
    expect(screen.getByText("FIX NOW")).toBeTruthy();
    expect(screen.getAllByText("Nothing to fix")).toHaveLength(2);
    expect(screen.getByText("5m")).toBeTruthy();
  });

  it("searches titles and projects", async () => {
    fakeAPI({ ...base(), "/api/sessions/cards": list() });
    renderAt("/sessions");
    await userEvent.type(await screen.findByLabelText("Search sessions"), "DOCS");
    expect(titles()).toEqual(["Write the guide"]);
  });

  it("shows the sessions that need attention, or are new", async () => {
    fakeAPI({ ...base(), "/api/sessions/cards": list() });
    renderAt("/sessions");
    await userEvent.click(await screen.findByRole("tab", { name: /Needs attention/ }));
    expect(titles()).toEqual(["Fix the login bug", "Speed up tests"]);
    await userEvent.click(screen.getByRole("tab", { name: /New/ }));
    expect(titles()).toEqual(["Fix the login bug"]);
  });

  it("sorts without grouping", async () => {
    fakeAPI({ ...base(), "/api/sessions/cards": list() });
    renderAt("/sessions");
    await userEvent.click(await screen.findByRole("button", { name: "Filters" }));
    await userEvent.click(screen.getByRole("button", { name: "None" }));
    const sort = screen.getByLabelText("Sort");
    await userEvent.selectOptions(sort, "newest");
    expect(titles()).toEqual(["Add dark mode", "Fix the login bug", "Write the guide", "Speed up tests"]);
    await userEvent.selectOptions(sort, "cost");
    expect(titles()[0]).toBe("Speed up tests");
    await userEvent.selectOptions(sort, "waiting");
    expect(titles().slice(0, 2)).toEqual(["Speed up tests", "Fix the login bug"]);
    expect(screen.getByText(/31K tok · web$/)).toBeTruthy();
  });

  it("filters by problem and symptom, and clears them", async () => {
    fakeAPI({ ...base(), "/api/sessions/cards": list() });
    renderAt("/sessions");
    await userEvent.click(await screen.findByRole("button", { name: "Filters" }));
    const pop = within(screen.getByRole("dialog", { name: "Filters" }));
    await userEvent.click(pop.getByLabelText("Permissions"));
    expect(titles()).toEqual(["Speed up tests"]);
    await userEvent.click(pop.getByLabelText("Long approval waits"));
    await userEvent.click(pop.getByLabelText("Compacted"));
    expect(screen.getByText("No sessions match")).toBeTruthy();
    await userEvent.click(pop.getByLabelText("Compacted"));
    await userEvent.click(pop.getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("button", { name: /Filters/ }).textContent).toBe("Filters2");

    await userEvent.click(screen.getByRole("button", { name: "Permissions" }));
    expect(titles()).toEqual(["Speed up tests"]);
    await userEvent.click(screen.getByRole("button", { name: "Long approval waits" }));
    expect(titles()).toHaveLength(4);

    await userEvent.click(screen.getByRole("button", { name: /Filters/ }));
    await userEvent.click(within(screen.getByRole("dialog")).getByLabelText("Model"));
    await userEvent.click(screen.getAllByRole("button", { name: "Clear all" })[0]);
    expect(titles()).toHaveLength(4);
  });

  it("clears everything from the empty state", async () => {
    fakeAPI({ ...base(), "/api/sessions/cards": list() });
    renderAt("/sessions");
    await userEvent.type(await screen.findByLabelText("Search sessions"), "nothing like this");
    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(titles()).toHaveLength(4);
  });

  it("loads the chosen time range", async () => {
    const calls = fakeAPI({ ...base(), "/api/sessions/cards": list() });
    renderAt("/sessions");
    await userEvent.click(await screen.findByRole("button", { name: "3 months" }));
    expect(await screen.findByText(/in the last 3 months/)).toBeTruthy();
    expect(screen.getByTestId("where").textContent).toBe("/sessions?range=3m");
    expect(calls.filter((c) => c.path.startsWith("/api/sessions/cards")).map((c) => c.path)).toContain("/api/sessions/cards?since=3m");
  });

  it("shows why it couldn't load", async () => {
    fakeAPI({ ...base(), "/api/sessions/cards": new HTTPError(500, "database is locked") });
    renderAt("/sessions");
    expect(await screen.findByText("Could not load: database is locked")).toBeTruthy();
  });

  it("explains how to start before anything has arrived", async () => {
    fakeAPI({ ...base(), "/api/status": status({ last_event_ms: undefined }), "/api/sessions/cards": list() });
    renderAt("/sessions");
    expect(await screen.findByRole("heading", { name: "Waiting for your first session." })).toBeTruthy();
  });
});
