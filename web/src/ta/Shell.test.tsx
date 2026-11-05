import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { aiConfig, base, card, cards, overview, status } from "../test/fixtures";
import { renderAt } from "../test/render";

describe("sidebar", () => {
  it("says the collector is live, prompts are logged and AI is on", async () => {
    fakeAPI({ ...base(), "/api/ai/config": aiConfig({ configured: true }) });
    renderAt("/settings");
    expect(await screen.findByText("Local only · collector live")).toBeTruthy();
    expect(screen.getByText("Prompt text: logged")).toBeTruthy();
    expect(await screen.findByText("AI insights · On")).toBeTruthy();
  });

  it("says when the collector is idle, prompts aren't logged and AI is off", async () => {
    fakeAPI({ ...base(), "/api/status": status({ collector_live: false, prompts_logged_7d: false }), "/api/ai/config": new HTTPError(500, "x") });
    renderAt("/settings");
    expect(await screen.findByText("Local only · collector idle")).toBeTruthy();
    expect(screen.getByText("Prompt text: not logged")).toBeTruthy();
    expect(screen.getByText("AI insights · Off — add key")).toBeTruthy();
  });

  it("is still checking when the status can't be read", async () => {
    fakeAPI({ ...base(), "/api/status": new HTTPError(500, "x") });
    renderAt("/settings");
    await screen.findAllByText(/API key/);
    expect(screen.getByText("Checking collector…")).toBeTruthy();
  });

  it("has no theme switch", async () => {
    fakeAPI(base());
    renderAt("/");
    await screen.findByText("Local only · collector live");
    expect(screen.queryByRole("button", { name: /Switch to (light|dark) mode/ })).toBeNull();
  });

  it("marks the open page in the nav, with counts", async () => {
    fakeAPI({ ...base(), "/api/overview": overview(), "/api/sessions/cards": cards([card()]) });
    renderAt("/");
    await screen.findByText("Fix first");
    expect(screen.getByRole("link", { name: /^Overview/ }).className).toBe("on");
    expect(screen.getByRole("link", { name: /^Fixes/ }).className).toBe("");
    expect(screen.getByText("2 to do").className).toContain("amber");
  });

  it("lists recent sessions by project, and folds the list away", async () => {
    fakeAPI({
      ...base(),
      "/api/sessions/cards": cards([
        card(),
        card({ id: "s2", title: '<pasted_content id="1"> tidy the docs', priority: "fine", started_at: 1 }),
        card({ id: "s3", project: "web", title: "Restyle", priority: "look" }),
      ]),
    });
    renderAt("/settings");
    const recents = within(await screen.findByRole("region", { name: "Recent sessions" }));
    expect(await recents.findByText("tidy the docs")).toBeTruthy();
    expect(recents.getByText("api")).toBeTruthy();
    expect(recents.getByText("web")).toBeTruthy();
    expect(recents.getByRole("link", { name: /Fix the login bug/ }).getAttribute("href")).toBe("/sessions/s1");
    await userEvent.selectOptions(screen.getByLabelText("Sort recent sessions"), "cost");
    expect(localStorage.getItem("ta-side-sort")).toBe("cost");
    await userEvent.click(recents.getByRole("button", { name: "Recents" }));
    expect(recents.queryByText("tidy the docs")).toBeNull();
    expect(localStorage.getItem("ta-recents-open")).toBe("0");
  });

  it("changes the time range from the sidebar", async () => {
    const calls = fakeAPI({ ...base(), "/api/sessions/cards": cards([]) });
    renderAt("/settings");
    await screen.findByText("No sessions in this range.");
    await userEvent.selectOptions(screen.getByLabelText("Time range"), "30d");
    await waitFor(() => expect(calls.some((c) => c.path === "/api/sessions/cards?since=30d")).toBe(true));
  });
});
