import { vi } from "vitest";

// A Reply is what the fake API answers for one path: a JSON body, or a
// function of the request's body for answers that change.
type Reply = unknown | ((body: unknown) => unknown);

export type Call = { method: string; path: string; body: unknown };

// HTTPError makes the fake API answer with an error status.
export class HTTPError {
  status: number;
  error: string;
  constructor(status: number, error: string) {
    this.status = status;
    this.error = error;
  }
}

// fakeAPI replaces fetch with one that answers from routes, keyed by "path" or
// "METHOD path" without the query string. Paths it doesn't know get a 404, so a
// page that asks for something unexpected shows it. It returns every call made.
export function fakeAPI(routes: Record<string, Reply>): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal("fetch", async (input: string, init?: RequestInit) => {
    const url = new URL(input, "http://localhost");
    const method = init?.method ?? "GET";
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    calls.push({ method, path: url.pathname + url.search, body });
    const key = `${method} ${url.pathname}` in routes ? `${method} ${url.pathname}` : url.pathname;
    if (!(key in routes)) return json(404, { error: `no route for ${method} ${url.pathname}` });
    const route = routes[key];
    const reply = typeof route === "function" ? route(body) : route;
    if (reply instanceof HTTPError) return json(reply.status, { error: reply.error });
    return json(200, reply);
  });
  return calls;
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}
