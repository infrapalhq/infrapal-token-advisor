import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { aiConfig, base, status } from "../test/fixtures";
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

  it("switches between dark and light", async () => {
    fakeAPI(base());
    renderAt("/settings");
    await userEvent.click(await screen.findByRole("button", { name: "Switch to light mode" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    await userEvent.click(screen.getByRole("button", { name: "Switch to dark mode" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});
