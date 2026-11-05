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
    const calls = fakeAPI({ "POST /api/fixes/k/state": {}, "POST /api/ui/visit": {} });
    await api.setFixState("k", "reverted");
    await api.visit();
    expect(calls.map((c) => c.body)).toEqual([{ state: "reverted" }, undefined]);
  });

  it("throws the API's error message", async () => {
    fakeAPI({ "/api/status": new HTTPError(500, "database is locked"), "POST /api/fixes/k/state": new HTTPError(400, "unknown state") });
    await expect(api.status()).rejects.toThrow("database is locked");
    await expect(api.setFixState("k", "fixed")).rejects.toThrow("unknown state");
  });

  it("falls back to the status code when the error isn't JSON", async () => {
    vi.stubGlobal("fetch", async () => new Response("<html>bad gateway</html>", { status: 502 }));
    await expect(api.agents()).rejects.toThrow("HTTP 502");
    await expect(api.visit()).rejects.toThrow("HTTP 502");
  });
});
