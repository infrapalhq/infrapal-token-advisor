import { describe, expect, it, vi } from "vitest";
import { HTTPError, fakeAPI } from "../test/api";
import { api } from "./api";

describe("api", () => {
  it("adds the agent to queries only when one is chosen", async () => {
    const calls = fakeAPI({ "/api/overview": {}, "/api/fixes": {} });
    await api.overview("30d");
    await api.overview("7d", "codex cli");
    await api.fixes("all", "");
    expect(calls.map((c) => c.path)).toEqual([
      "/api/overview?since=30d",
      "/api/overview?since=7d&agent=codex%20cli",
      "/api/fixes?since=all",
    ]);
  });

  it("escapes session ids and fix keys in paths", async () => {
    const calls = fakeAPI({ "/api/sessions/a%2Fb/timeline": {}, "POST /api/fixes/reread%7Csrc%2Fa.ts/state": {} });
    await api.timeline("a/b");
    await api.setFixState("reread|src/a.ts", "fixed");
    expect(calls[0].path).toBe("/api/sessions/a%2Fb/timeline");
    expect(calls[1]).toEqual({ method: "POST", path: "/api/fixes/reread%7Csrc%2Fa.ts/state", body: { state: "fixed" } });
  });

  it("sends JSON bodies, and no body when there's nothing to send", async () => {
    const calls = fakeAPI({ "PUT /api/ai/config": {}, "POST /api/ui/visit": {}, "POST /api/ai/recommendations": {} });
    await api.saveAIKey("k");
    await api.chooseAIProvider("openai");
    await api.visit();
    await api.generateAI("30d", true);
    expect(calls.map((c) => c.body)).toEqual([{ api_key: "k" }, { provider: "openai" }, undefined, { since: "30d", force: true }]);
  });

  it("throws the API's error message", async () => {
    fakeAPI({ "/api/status": new HTTPError(500, "database is locked"), "PUT /api/ai/config": new HTTPError(400, "key is too short") });
    await expect(api.status()).rejects.toThrow("database is locked");
    await expect(api.saveAIKey("x")).rejects.toThrow("key is too short");
  });

  it("falls back to the status code when the error isn't JSON", async () => {
    vi.stubGlobal("fetch", async () => new Response("<html>bad gateway</html>", { status: 502 }));
    await expect(api.agents()).rejects.toThrow("HTTP 502");
    await expect(api.visit()).rejects.toThrow("HTTP 502");
  });
});
