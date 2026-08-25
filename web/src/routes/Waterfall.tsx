import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { PageShell } from "../components/PageShell";
import { Inspector } from "../components/Inspector";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { Toolbar } from "../components/Toolbar";
import { type TimeRange } from "../lib/timeRange";
import {
  listInteractions,
  getInteraction,
  type InteractionSummary,
  type InteractionDetail,
} from "../lib/api";
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
  formatTimestamp,
  formatTokenCount,
  summarizePromptText,
} from "../lib/format";

const FALLBACK_PX_PER_MS = 0.4;
const ROW_HEIGHT_PX = 34;
const PAGE_SIZE = 12;

function InteractionCard({
  item,
  onOpen,
}: {
  item: InteractionSummary;
  onOpen: () => void;
}) {
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
      </div>
    </button>
  );
}

export default function Waterfall() {
  const [params, setParams] = useSearchParams();
  const promptId = params.get("prompt") ?? "";

  const [page, setPage] = useState(0);
  const [range, setRange] = useState<TimeRange>("30d");
  const [items, setItems] = useState<InteractionSummary[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [detail, setDetail] = useState<InteractionDetail | null>(null);
  const [selected, setSelected] = useState<WaterfallRow | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [conversationOpen, setConversationOpen] = useState(true);
  const timelineRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(0);

  function changeRange(next: TimeRange) {
    setRange(next);
    setPage(0);
  }

  useEffect(() => {
    if (promptId) return;
    listInteractions(PAGE_SIZE + 1, page * PAGE_SIZE, range || undefined)
      .then((fetched) => {
        setHasMore(fetched.length > PAGE_SIZE);
        setItems(fetched.slice(0, PAGE_SIZE));
      })
      .catch((e) => setError(String(e)));
  }, [page, range, promptId]);

  useEffect(() => {
    setSelected(null);
    if (!promptId) {
      setDetail(null);
      return;
    }
    getInteraction(promptId)
      .then(setDetail)
      .catch((e) => setError(String(e)));
  }, [promptId]);

  const rows = useMemo(() => {
    if (!detail) return [];
    return buildWaterfallRows(
      detail.interaction,
      detail.api_requests,
      detail.tool_calls,
    );
  }, [detail]);

  const tokenTotals = useMemo(() => {
    if (!detail) return null;
    let input = 0;
    let output = 0;
    let cacheRead = 0;
    let cacheCreation = 0;
    for (const r of detail.api_requests) {
      input += r.input_tokens ?? 0;
      output += r.output_tokens ?? 0;
      cacheRead += r.cache_read_tokens ?? 0;
      cacheCreation += r.cache_creation_tokens ?? 0;
    }
    return {
      input,
      output,
      cacheRead,
      cacheCreation,
      total: input + output + cacheRead + cacheCreation,
    };
  }, [detail]);

  const totalMs = detail ? totalDurationMs(detail.interaction, rows) : 0;

  // Fit the whole interaction into the visible width by default — a fixed
  // px-per-ms scale left most of a trace scrolled off to the right (long
  // idle/tool gaps dwarf the visible viewport), which is what made the
  // waterfall read as a plain list instead of a timeline.
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

  const pxPerMs =
    totalMs > 0 && containerWidth > 0 ? containerWidth / totalMs : FALLBACK_PX_PER_MS;
  const timelineWidth = containerWidth > 0 ? containerWidth : totalMs * FALLBACK_PX_PER_MS;
  const tickInterval = niceTickIntervalMs(totalMs);
  const ticks = useMemo(() => {
    const out: number[] = [];
    for (let t = 0; t <= totalMs; t += tickInterval) out.push(t);
    return out;
  }, [totalMs, tickInterval]);

  return (
    <PageShell
      title="Interaction waterfall"
      subtitle="Prompt-to-response timeline: tool calls, subagents, and MCP spans"
    >
      {error && <p className="text-sm text-fault">{error}</p>}

      {!promptId && (
        <>
          <Toolbar left={<TimeRangeSelect value={range} onChange={changeRange} />} />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((item) => (
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
          {items.length === 0 && (
            <div className="empty">No interactions recorded yet.</div>
          )}
          <div className="flex items-center justify-between text-sm">
            <button
              type="button"
              disabled={page === 0}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              className="btn disabled:opacity-40"
            >
              ← Prev
            </button>
            <span className="text-ink/50">Page {page + 1}</span>
            <button
              type="button"
              disabled={!hasMore}
              onClick={() => setPage((p) => p + 1)}
              className="btn disabled:opacity-40"
            >
              Next →
            </button>
          </div>
        </>
      )}

      {promptId && (
        <>
          <button
            type="button"
            onClick={() => setParams(new URLSearchParams())}
            className="mb-4 text-sm text-signal hover:underline"
          >
            ← Back to interactions
          </button>

          {!detail && (
            <div className="flex h-64 items-center justify-center rounded border border-dashed border-wire text-sm text-ink/50">
              Loading interaction…
            </div>
          )}

          {detail && tokenTotals && (
            <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-5">
              <div className="card p-3.5">
                <div className="kpi-label">Total tokens</div>
                <div className="mt-1 font-mono text-lg text-ink">
                  {formatTokenCount(tokenTotals.total)}
                </div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Input</div>
                <div className="mt-1 font-mono text-lg text-ink">
                  {formatTokenCount(tokenTotals.input)}
                </div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Output</div>
                <div className="mt-1 font-mono text-lg text-ink">
                  {formatTokenCount(tokenTotals.output)}
                </div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Cache read</div>
                <div className="mt-1 font-mono text-lg text-ink">
                  {formatTokenCount(tokenTotals.cacheRead)}
                </div>
              </div>
              <div className="card p-3.5">
                <div className="kpi-label">Cache creation</div>
                <div className="mt-1 font-mono text-lg text-ink">
                  {formatTokenCount(tokenTotals.cacheCreation)}
                </div>
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
                <span className="font-display text-sm font-semibold text-ink">
                  Conversation
                </span>
                <span className="text-xs text-ink/50">
                  {conversationOpen ? "Hide" : "Show"} full prompt +{" "}
                  {detail.assistant_responses.length} response
                  {detail.assistant_responses.length === 1 ? "" : "s"}
                </span>
              </button>
              {conversationOpen && (
                <div className="max-h-80 overflow-y-auto border-t border-wire px-4 py-3">
                  <div>
                    <div className="text-[11px] uppercase tracking-wide text-ink/50">
                      Prompt
                    </div>
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
                      <div
                        key={ar.message_uuid}
                        className="mt-3 border-t border-wire/40 pt-3"
                      >
                        <div className="flex items-center gap-2 text-[11px] uppercase tracking-wide text-ink/50">
                          <span>Response {idx + 1}</span>
                          {ar.model && (
                            <span className="font-mono text-cat-llm">
                              {ar.model}
                            </span>
                          )}
                          <span className="font-mono">
                            {formatTimestamp(ar.ts)}
                          </span>
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
                <div
                  className="border-b border-wire text-[11px] uppercase tracking-wide text-ink/50"
                  style={{ height: ROW_HEIGHT_PX }}
                />
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
                        <span className="truncate text-xs font-medium text-ink">
                          {row.label}
                        </span>
                        {row.apiRequest && (
                          <span className="shrink-0 font-mono text-[10px] text-ink/40">
                            {formatTokenCount(row.apiRequest.input_tokens)}→
                            {formatTokenCount(row.apiRequest.output_tokens)}
                          </span>
                        )}
                      </div>
                      {row.sublabel && (
                        <span className="truncate font-mono text-[10px] text-ink/50">
                          {row.sublabel}
                        </span>
                      )}
                    </button>
                  );
                })}
              </div>

              <div
                ref={timelineRef}
                className="min-w-0 flex-1 overflow-x-auto overflow-y-hidden"
              >
                <div style={{ width: timelineWidth }}>
                  <div
                    className="relative border-b border-wire"
                    style={{ height: ROW_HEIGHT_PX }}
                  >
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
                    const width = Math.max(
                      3,
                      (row.endMs - row.startMs) * pxPerMs,
                    );
                    const selectable = Boolean(row.toolCall || row.apiRequest);
                    const tokenNote = row.apiRequest
                      ? ` — ${formatTokenCount(row.apiRequest.input_tokens)} in / ${formatTokenCount(row.apiRequest.output_tokens)} out`
                      : "";
                    return (
                      <div
                        key={row.id}
                        className="relative border-b border-wire/40"
                        style={{ height: ROW_HEIGHT_PX }}
                      >
                        <div
                          onClick={() => selectable && setSelected(row)}
                          className={[
                            "absolute top-1.5 h-5 rounded-sm opacity-90",
                            rowColorClass(row.kind),
                            selectable ? "cursor-pointer hover:opacity-100" : "",
                          ].join(" ")}
                          style={{ left, width }}
                          title={`${row.label} — ${formatDurationMs(row.endMs - row.startMs)}${tokenNote}`}
                        >
                          {row.blockedStartMs != null &&
                            row.blockedEndMs != null && (
                              <div
                                className="hatch-fault absolute top-0 h-full rounded-sm"
                                style={{
                                  left: 0,
                                  width: Math.max(
                                    2,
                                    (row.blockedEndMs - row.blockedStartMs) *
                                      pxPerMs,
                                  ),
                                }}
                                title="Blocked on user"
                              />
                            )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>

              <Inspector
                row={selected}
                originMs={detail.interaction.started_at}
                onClose={() => setSelected(null)}
              />
            </div>
          )}
        </>
      )}
    </PageShell>
  );
}
