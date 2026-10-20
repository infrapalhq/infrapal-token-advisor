import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  ReferenceLine,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import { PageShell } from "../components/PageShell";
import { Inspector } from "../components/Inspector";
import { useDateRange } from "../lib/dateRange";
import {
  listInteractions,
  getInteraction,
  getView,
  type InteractionSummary,
  type InteractionDetail,
  type InteractionSort,
  type TurnContext,
} from "../lib/api";
import {
  getAiConfig,
  saveAiApiKey,
  clearAiApiKey,
  generateRecommendations,
  type AiRecommendation,
} from "../lib/apiAi";
import {
  buildWaterfallRows,
  totalDurationMs,
  rowColorClass,
  niceTickIntervalMs,
  type WaterfallRow,
} from "../lib/waterfall";
import {
  formatDurationMs,
  formatCostUsd,
  formatCostMicros,
  formatTimestamp,
  formatTokenCount,
  summarizePromptText,
} from "../lib/format";

const FALLBACK_PX_PER_MS = 0.4;
const ROW_HEIGHT_PX = 34;
const PAGE_SIZE = 12;

type Filter = "all" | "expensive" | "failed" | "rejected" | "long";

function matchesFilter(item: InteractionSummary, filter: Filter): boolean {
  switch (filter) {
    case "expensive":
      return item.total_cost_usd >= 0.5;
    case "failed":
      return item.tool_error_count > 0;
    case "rejected":
      return item.rejected_count > 0;
    case "long":
      return (item.duration_ms ?? 0) >= 120_000 || item.blocked_on_user_ms >= 60_000;
    default:
      return true;
  }
}

function InteractionCard({ item, onOpen }: { item: InteractionSummary; onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="card flex flex-col p-4 text-left transition-colors hover:border-signal/60"
    >
      <div className="flex items-center justify-between text-[11px] font-mono text-ink/50">
        <span>{item.session_id.slice(0, 8)}</span>
        <span>{formatTimestamp(item.started_at)}</span>
      </div>
      <p className="mt-2 line-clamp-3 text-sm text-ink">
        {item.command_name ? (
          <span className="font-mono text-signal">{item.command_name}</span>
        ) : (
          summarizePromptText(item.prompt_text)
        )}
      </p>
      <div className="mt-3 flex flex-wrap gap-3 border-t border-wire/40 pt-2 text-[11px] font-mono text-ink/60">
        <span>{formatCostUsd(item.total_cost_usd)}</span>
        <span>{item.total_tokens.toLocaleString()} tok</span>
        <span>{item.llm_turn_count} turns</span>
        <span>{item.tool_call_count} tools</span>
        {item.rejected_count > 0 && <span className="text-fault">{item.rejected_count} rejected</span>}
        {item.tool_error_count > 0 && <span className="text-fault">{item.tool_error_count} errors</span>}
      </div>
    </button>
  );
}

function ExplainTracePanel({ interactionStartedAt }: { interactionStartedAt: number }) {
  const [open, setOpen] = useState(false);
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [keyInput, setKeyInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [recs, setRecs] = useState<AiRecommendation[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || configured != null) return;
    getAiConfig()
      .then((c) => setConfigured(c.configured))
      .catch(() => setConfigured(false));
  }, [open, configured]);

  function handleSaveKey() {
    if (!keyInput.trim()) return;
    setBusy(true);
    setError(null);
    saveAiApiKey(keyInput.trim())
      .then((c) => {
        setConfigured(c.configured);
        setKeyInput("");
      })
      .catch((e) => setError(String(e)))
      .finally(() => setBusy(false));
  }

  function handleRemoveKey() {
    setBusy(true);
    setError(null);
    clearAiApiKey()
      .then((c) => {
        setConfigured(c.configured);
        setRecs(null);
      })
      .catch((e) => setError(String(e)))
      .finally(() => setBusy(false));
  }

  function handleGenerate() {
    // No backend support yet for scoping the AI context to exactly one
    // conversation — the closest honest approximation is "since this
    // conversation's day", which the copy below states plainly rather than
    // implying more precision than this actually has.
    const days = Math.max(1, Math.ceil((Date.now() - interactionStartedAt) / 86_400_000) + 1);
    setBusy(true);
    setError(null);
    generateRecommendations(`${days}d`, false)
      .then((r) => setRecs(r.recommendations))
      .catch((e) => setError(String(e)))
      .finally(() => setBusy(false));
  }

  if (!open) {
    return (
      <button type="button" className="btn" onClick={() => setOpen(true)}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
          <path d="M12 2l1.8 5.4L19 9l-5.2 1.6L12 16l-1.8-5.4L5 9l5.2-1.6L12 2z" />
        </svg>
        Explain this trace
      </button>
    );
  }

  return (
    <div className="card card-pad">
      <div className="flex items-center justify-between">
        <div className="card-title">Explain this trace</div>
        <button type="button" className="text-ink/50 hover:text-ink" onClick={() => setOpen(false)} aria-label="Close">
          ✕
        </button>
      </div>

      {error && <p className="mt-2 text-sm text-fault">{error}</p>}

      {configured === false && (
        <div className="mt-3">
          <p className="card-desc">
            Add an Anthropic API key to enable this — it's sent nowhere except directly to the Anthropic API, only
            when you click Generate.
          </p>
          <div className="mt-2 flex gap-2">
            <input
              type="password"
              value={keyInput}
              onChange={(e) => setKeyInput(e.target.value)}
              placeholder="sk-ant-…"
              className="text-input flex-1"
            />
            <button type="button" className="btn-primary" disabled={busy} onClick={handleSaveKey}>
              Save
            </button>
          </div>
        </div>
      )}

      {configured === true && (
        <div className="mt-3">
          <div className="flex items-center gap-2">
            <button type="button" className="btn-primary" disabled={busy} onClick={handleGenerate}>
              {busy ? "Generating…" : "Generate"}
            </button>
            <button type="button" className="btn" disabled={busy} onClick={handleRemoveKey}>
              Remove key
            </button>
          </div>
          <p className="card-desc mt-2">
            Scoped to your activity since this conversation happened, not this conversation alone — the deterministic
            findings context this reuses isn't yet filterable to a single prompt.
          </p>
          {recs && (
            <div className="mt-3 flex flex-col gap-2">
              {recs.length === 0 && <div className="empty">Nothing to flag in that window.</div>}
              {recs.map((r) => (
                <div key={r.id} className="rounded-md border border-wire bg-panel-2 p-3">
                  <div className="text-sm font-semibold text-ink">{r.title}</div>
                  <p className="mt-1 text-[13px] leading-relaxed text-ink/70">{r.detail}</p>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function Trace() {
  const [params, setParams] = useSearchParams();
  const promptId = params.get("prompt") ?? "";
  const sessionParam = params.get("session") ?? "";
  const { range } = useDateRange();

  const [page, setPage] = useState(0);
  const [filter, setFilter] = useState<Filter>("all");
  const [sort, setSort] = useState<InteractionSort>("cost");
  const [items, setItems] = useState<InteractionSummary[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [detail, setDetail] = useState<InteractionDetail | null>(null);
  const [turnContext, setTurnContext] = useState<TurnContext[]>([]);
  const [selected, setSelected] = useState<WaterfallRow | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [conversationOpen, setConversationOpen] = useState(true);
  const timelineRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(0);

  useEffect(() => {
    setPage(0);
  }, [range, sort]);

  useEffect(() => {
    if (promptId) return;
    listInteractions(PAGE_SIZE + 1, page * PAGE_SIZE, range || undefined, sort)
      .then((fetched) => {
        setHasMore(fetched.length > PAGE_SIZE);
        setItems(fetched.slice(0, PAGE_SIZE));
      })
      .catch((e) => setError(String(e)));
  }, [page, promptId, range, sort]);

  useEffect(() => {
    setSelected(null);
    if (!promptId) {
      setDetail(null);
      setTurnContext([]);
      return;
    }
    getInteraction(promptId)
      .then(setDetail)
      .catch((e) => setError(String(e)));
  }, [promptId]);

  useEffect(() => {
    if (!detail) return;
    getView<TurnContext>("turn_context", { session_id: detail.interaction.session_id, limit: "1000" })
      .then((rows) => setTurnContext([...rows].sort((a, b) => a.seq - b.seq)))
      .catch(() => setTurnContext([]));
  }, [detail]);

  const rows = useMemo(() => {
    if (!detail) return [];
    return buildWaterfallRows(detail.interaction, detail.api_requests, detail.tool_calls);
  }, [detail]);

  const maxCostMicros = useMemo(
    () => Math.max(1, ...rows.map((r) => r.apiRequest?.cost_usd_micros ?? 0)),
    [rows],
  );

  const tokenTotals = useMemo(() => {
    if (!detail) return null;
    let input = 0, output = 0, cacheRead = 0, cacheCreation = 0;
    for (const r of detail.api_requests) {
      input += r.input_tokens ?? 0;
      output += r.output_tokens ?? 0;
      cacheRead += r.cache_read_tokens ?? 0;
      cacheCreation += r.cache_creation_tokens ?? 0;
    }
    return { input, output, cacheRead, cacheCreation, total: input + output + cacheRead + cacheCreation };
  }, [detail]);

  const totalMs = detail ? totalDurationMs(detail.interaction, rows) : 0;

  useEffect(() => {
    const el = timelineRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width;
      if (width) setContainerWidth(width);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [detail]);

  const pxPerMs = totalMs > 0 && containerWidth > 0 ? containerWidth / totalMs : FALLBACK_PX_PER_MS;
  const timelineWidth = containerWidth > 0 ? containerWidth : totalMs * FALLBACK_PX_PER_MS;
  const tickInterval = niceTickIntervalMs(totalMs);
  const ticks = useMemo(() => {
    const out: number[] = [];
    for (let t = 0; t <= totalMs; t += tickInterval) out.push(t);
    return out;
  }, [totalMs, tickInterval]);

  const contextChartData = useMemo(
    () =>
      turnContext.map((t, idx) => ({
        turn: idx + 1,
        peak: t.peak_context_tokens,
        flagged: Boolean(t.triggered_compaction || t.hit_max_tokens),
        isCurrent: t.prompt_id === promptId,
      })),
    [turnContext, promptId],
  );
  const contextPeak = Math.max(1, ...contextChartData.map((d) => d.peak));

  const filteredItems = useMemo(() => items.filter((i) => matchesFilter(i, filter)), [items, filter]);

  return (
    <PageShell
      title="Trace"
      subtitle="Prompt-to-response timeline: tool calls, subagents, and MCP spans"
    >
      {error && <p className="text-sm text-fault">{error}</p>}

      {!promptId && (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap gap-2">
              {(["all", "expensive", "failed", "rejected", "long"] as Filter[]).map((f) => (
                <button
                  key={f}
                  type="button"
                  className={`chip capitalize ${filter === f ? "chip-active" : ""}`}
                  onClick={() => setFilter(f)}
                >
                  {f === "long" ? "Long-running" : f}
                </button>
              ))}
            </div>
            <label className="flex items-center gap-2 text-[13px] text-ink/55">
              Sort by
              <select
                value={sort}
                onChange={(e) => setSort(e.target.value as InteractionSort)}
                className="select h-8 text-[13px]"
              >
                <option value="cost">Cost</option>
                <option value="errors">Errors</option>
                <option value="started_at">Newest</option>
              </select>
            </label>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {filteredItems.map((item) => (
              <InteractionCard
                key={item.prompt_id}
                item={item}
                onOpen={() => {
                  const next = new URLSearchParams();
                  next.set("prompt", item.prompt_id);
                  setParams(next);
                }}
              />
            ))}
          </div>
          {filteredItems.length === 0 && (
            <div className="empty">
              {items.length === 0 ? "No interactions recorded yet." : "No interactions match this filter."}
            </div>
          )}
          <div className="flex items-center justify-between text-sm">
            <button type="button" disabled={page === 0} onClick={() => setPage((p) => Math.max(0, p - 1))} className="btn disabled:opacity-40">
              ← Prev
            </button>
            <span className="text-ink/50">Page {page + 1}</span>
            <button type="button" disabled={!hasMore} onClick={() => setPage((p) => p + 1)} className="btn disabled:opacity-40">
              Next →
            </button>
          </div>
        </>
      )}

      {promptId && (
        <>
          <div className="mb-4 flex items-center justify-between">
            <button
              type="button"
              onClick={() => setParams(sessionParam ? new URLSearchParams({ session: sessionParam }) : new URLSearchParams())}
              className="text-sm text-signal hover:underline"
            >
              ← Back to interactions
            </button>
            {detail && <ExplainTracePanel interactionStartedAt={detail.interaction.started_at} />}
          </div>

          {!detail && (
            <div className="flex h-64 items-center justify-center rounded border border-dashed border-wire text-sm text-ink/50">
              Loading interaction…
            </div>
          )}

          {detail && tokenTotals && (
            <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-5">
              <div className="card p-3.5">
                <div className="kpi-label">Total tokens</div>
                <div className="mt-1 font-mono text-lg text-ink">{formatTokenCount(tokenTotals.total)}</div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Input</div>
                <div className="mt-1 font-mono text-lg text-ink">{formatTokenCount(tokenTotals.input)}</div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Output</div>
                <div className="mt-1 font-mono text-lg text-ink">{formatTokenCount(tokenTotals.output)}</div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Cache read</div>
                <div className="mt-1 font-mono text-lg text-ink">{formatTokenCount(tokenTotals.cacheRead)}</div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Cache creation</div>
                <div className="mt-1 font-mono text-lg text-ink">{formatTokenCount(tokenTotals.cacheCreation)}</div>
              </div>
            </div>
          )}

          {detail && (
            <div className="mb-4 rounded border border-wire bg-panel">
              <button
                type="button"
                onClick={() => setConversationOpen((o) => !o)}
                className="flex w-full items-center justify-between px-4 py-2.5 text-left"
              >
                <span className="font-display text-sm font-semibold text-ink">Conversation</span>
                <span className="text-xs text-ink/50">
                  {conversationOpen ? "Hide" : "Show"} full prompt + {detail.assistant_responses.length} response
                  {detail.assistant_responses.length === 1 ? "" : "s"}
                </span>
              </button>
              {conversationOpen && (
                <div className="max-h-80 overflow-y-auto border-t border-wire px-4 py-3">
                  <div>
                    <div className="text-[11px] uppercase tracking-wide text-ink/50">Prompt</div>
                    <p className="mt-1 whitespace-pre-wrap text-sm text-ink">
                      {summarizePromptText(detail.interaction.prompt_text)}
                    </p>
                  </div>
                  {detail.assistant_responses.length === 0 ? (
                    <p className="mt-3 border-t border-wire/40 pt-3 text-sm text-ink/50">
                      No assistant response recorded for this prompt.
                    </p>
                  ) : (
                    detail.assistant_responses.map((ar, idx) => (
                      <div key={ar.message_uuid} className="mt-3 border-t border-wire/40 pt-3">
                        <div className="flex items-center gap-2 text-[11px] uppercase tracking-wide text-ink/50">
                          <span>Response {idx + 1}</span>
                          {ar.model && <span className="font-mono text-cat-llm">{ar.model}</span>}
                          <span className="font-mono">{formatTimestamp(ar.ts)}</span>
                        </div>
                        <p className="mt-1 whitespace-pre-wrap text-sm text-ink">
                          {ar.response_text || "(no response text)"}
                        </p>
                      </div>
                    ))
                  )}
                </div>
              )}
            </div>
          )}

          {detail && (
            <div className="relative flex overflow-hidden rounded border border-wire">
              <div className="w-56 shrink-0 border-r border-wire bg-panel">
                <div className="border-b border-wire text-[11px] uppercase tracking-wide text-ink/50" style={{ height: ROW_HEIGHT_PX }} />
                {rows.map((row) => {
                  const selectable = Boolean(row.toolCall || row.apiRequest);
                  return (
                    <button
                      key={row.id}
                      type="button"
                      onClick={() => selectable && setSelected(row)}
                      className={[
                        "flex w-full flex-col justify-center border-b border-wire/40 px-2 text-left",
                        row.depth > 0 ? "pl-6" : "",
                        selectable ? "cursor-pointer hover:bg-wire/30" : "cursor-default",
                      ].join(" ")}
                      style={{ height: ROW_HEIGHT_PX }}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="truncate text-xs font-medium text-ink">{row.label}</span>
                        {row.apiRequest && (
                          <span className="shrink-0 font-mono text-[10px] text-ink/40">
                            {formatTokenCount(row.apiRequest.input_tokens)}→{formatTokenCount(row.apiRequest.output_tokens)}
                          </span>
                        )}
                      </div>
                      {row.sublabel && <span className="truncate font-mono text-[10px] text-ink/50">{row.sublabel}</span>}
                    </button>
                  );
                })}
              </div>

              <div ref={timelineRef} className="min-w-0 flex-1 overflow-x-auto overflow-y-hidden">
                <div style={{ width: timelineWidth }}>
                  <div className="relative border-b border-wire" style={{ height: ROW_HEIGHT_PX }}>
                    {ticks.map((t) => (
                      <div
                        key={t}
                        className="absolute top-0 h-full border-l border-wire/50 pl-1 text-[10px] text-ink/40"
                        style={{ left: t * pxPerMs }}
                      >
                        {formatDurationMs(t)}
                      </div>
                    ))}
                  </div>

                  {rows.map((row) => {
                    const left = Math.max(0, row.startMs) * pxPerMs;
                    const width = Math.max(3, (row.endMs - row.startMs) * pxPerMs);
                    const selectable = Boolean(row.toolCall || row.apiRequest);
                    const tokenNote = row.apiRequest
                      ? ` — ${formatTokenCount(row.apiRequest.input_tokens)} in / ${formatTokenCount(row.apiRequest.output_tokens)} out`
                      : "";
                    // Colour intensity encodes cost for LLM spans (the only
                    // rows with a real per-call cost); tool/mcp/subagent
                    // spans have no per-call cost, so they render at full
                    // intensity and rely on hue alone for identity.
                    const costMicros = row.apiRequest?.cost_usd_micros ?? null;
                    const opacity = costMicros != null ? 0.4 + 0.6 * Math.min(1, costMicros / maxCostMicros) : 0.9;
                    return (
                      <div key={row.id} className="relative border-b border-wire/40" style={{ height: ROW_HEIGHT_PX }}>
                        <div
                          onClick={() => selectable && setSelected(row)}
                          className={["absolute top-1.5 h-5 rounded-sm", rowColorClass(row.kind), selectable ? "cursor-pointer hover:opacity-100" : ""].join(" ")}
                          style={{ left, width, opacity }}
                          title={`${row.label} — ${formatDurationMs(row.endMs - row.startMs)}${tokenNote}${costMicros != null ? ` — ${formatCostMicros(costMicros)}` : ""}`}
                        />
                        {row.blockedStartMs != null && row.blockedEndMs != null && (
                          <div
                            className="hatch-fault absolute top-1.5 h-5 rounded-sm ring-1 ring-fault"
                            style={{
                              left: Math.max(0, row.blockedStartMs) * pxPerMs,
                              width: Math.max(3, (row.blockedEndMs - row.blockedStartMs) * pxPerMs),
                            }}
                            title="Blocked on user"
                          />
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>

              <Inspector row={selected} originMs={detail.interaction.started_at} onClose={() => setSelected(null)} />
            </div>
          )}

          {detail && contextChartData.length > 1 && (
            <div className="card mt-4">
              <div className="card-head">
                <div>
                  <div className="card-title">Context growth within this conversation</div>
                  <div className="card-desc">
                    Peak context size per turn (input + cache) — red dots mark a compaction or max-tokens event
                  </div>
                </div>
              </div>
              <div className="card-pad">
                <div className="h-56">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={contextChartData} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
                      <CartesianGrid vertical={false} stroke="var(--color-wire)" strokeDasharray="3 4" />
                      <XAxis
                        dataKey="turn"
                        stroke="var(--color-wire)"
                        tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                        label={{ value: "turn", position: "insideBottom", offset: -2, fill: "var(--color-ink)", fontSize: 11 }}
                      />
                      <YAxis
                        stroke="var(--color-wire)"
                        tick={{ fill: "var(--color-ink)", fontSize: 11 }}
                        tickFormatter={(v) => formatTokenCount(Number(v))}
                        width={48}
                      />
                      <Tooltip
                        contentStyle={{ background: "var(--color-panel-2)", border: "1px solid var(--color-wire-2)", borderRadius: 6, fontSize: 12 }}
                        labelStyle={{ color: "var(--color-ink)" }}
                        formatter={(v) => [formatTokenCount(Number(v ?? 0)), "peak context"]}
                      />
                      <ReferenceLine
                        y={contextPeak}
                        stroke="var(--color-wire-2)"
                        label={{ value: `peak this conversation: ${formatTokenCount(contextPeak)}`, position: "insideTopRight", fill: "var(--color-ink)", fontSize: 11 }}
                      />
                      <Line
                        type="monotone"
                        dataKey="peak"
                        stroke="var(--color-chart)"
                        strokeWidth={2}
                        isAnimationActive={false}
                        dot={(props: { cx?: number; cy?: number; payload?: { flagged: boolean }; index?: number }) => {
                          const { cx, cy, payload, index } = props;
                          if (cx == null || cy == null) return <g key={index} />;
                          const flagged = Boolean(payload?.flagged);
                          return <circle key={index} cx={cx} cy={cy} r={flagged ? 5 : 2.5} fill={flagged ? "var(--color-fault)" : "var(--color-chart)"} />;
                        }}
                      />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              </div>
            </div>
          )}
        </>
      )}
    </PageShell>
  );
}
