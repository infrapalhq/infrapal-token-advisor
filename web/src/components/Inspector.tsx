import type { ReactNode } from "react";
import type { WaterfallRow } from "../lib/waterfall";
import { formatDurationMs, formatCostMicros, formatTimestamp } from "../lib/format";

function Field({ label, value }: { label: string; value: ReactNode }) {
  if (value == null || value === "") return null;
  return (
    <div className="border-b border-wire/60 py-2">
      <div className="text-[11px] uppercase tracking-wide text-ink/50">
        {label}
      </div>
      <div className="mt-0.5 break-words font-mono text-sm text-ink">
        {value}
      </div>
    </div>
  );
}

export function Inspector({
  row,
  originMs,
  onClose,
}: {
  row: WaterfallRow | null;
  originMs: number;
  onClose: () => void;
}) {
  return (
    <div
      className={[
        "absolute inset-y-0 right-0 z-20 w-96 max-w-full border-l border-wire bg-panel shadow-2xl transition-transform duration-200",
        row ? "translate-x-0" : "translate-x-full",
      ].join(" ")}
    >
      {row && (
        <div className="flex h-full flex-col">
          <div className="flex items-center justify-between border-b border-wire px-4 py-3">
            <h2 className="font-display text-sm font-semibold text-ink">
              {row.label}
            </h2>
            <button
              type="button"
              onClick={onClose}
              className="text-ink/50 hover:text-ink"
              aria-label="Close inspector"
            >
              ✕
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-4">
            <Field label="Kind" value={row.kind} />
            <Field
              label="Start"
              value={formatTimestamp(originMs + row.startMs)}
            />
            <Field
              label="Duration"
              value={formatDurationMs(row.endMs - row.startMs)}
            />
            {row.toolCall && (
              <>
                <Field label="Tool name" value={row.toolCall.tool_name} />
                <Field label="Tool source" value={row.toolCall.tool_source} />
                <Field label="Decision" value={row.toolCall.decision} />
                <Field
                  label="Decision source"
                  value={row.toolCall.decision_source}
                />
                <Field
                  label="Blocked on user"
                  value={formatDurationMs(row.toolCall.blocked_on_user_ms)}
                />
                <Field
                  label="Success"
                  value={
                    row.toolCall.success == null
                      ? null
                      : row.toolCall.success ? "yes" : "no"
                  }
                />
                <Field label="Error type" value={row.toolCall.error_type} />
                <Field label="Error" value={row.toolCall.error} />
                <Field label="MCP server" value={row.toolCall.mcp_server_name} />
                <Field label="MCP tool" value={row.toolCall.mcp_tool_name} />
                <Field label="Skill" value={row.toolCall.skill_name} />
                <Field label="Subagent type" value={row.toolCall.subagent_type} />
                <Field label="Command" value={row.toolCall.full_command} />
              </>
            )}
            {row.apiRequest && (
              <>
                <Field label="Model" value={row.apiRequest.model} />
                <Field label="Query source" value={row.apiRequest.query_source} />
                <Field label="Speed" value={row.apiRequest.speed} />
                <Field label="Effort" value={row.apiRequest.effort} />
                <Field label="Input tokens" value={row.apiRequest.input_tokens} />
                <Field label="Output tokens" value={row.apiRequest.output_tokens} />
                <Field
                  label="Cache read tokens"
                  value={row.apiRequest.cache_read_tokens}
                />
                <Field
                  label="Cache creation tokens"
                  value={row.apiRequest.cache_creation_tokens}
                />
                <Field
                  label="Cost"
                  value={formatCostMicros(row.apiRequest.cost_usd_micros)}
                />
                <Field label="TTFT" value={formatDurationMs(row.apiRequest.ttft_ms)} />
                <Field label="Stop reason" value={row.apiRequest.stop_reason} />
                <Field label="Attempt" value={row.apiRequest.attempt} />
                <Field label="Status code" value={row.apiRequest.status_code} />
                <Field label="Error" value={row.apiRequest.error} />
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
