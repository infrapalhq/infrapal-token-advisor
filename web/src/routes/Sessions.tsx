import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { PageShell } from "../components/PageShell";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { type TimeRange } from "../lib/timeRange";
import { listSessions, type Session } from "../lib/api";
import { formatCostUsd, formatDurationMs, formatTimestamp } from "../lib/format";

type SortKey =
  | "started_at"
  | "duration_ms"
  | "total_cost_usd"
  | "total_tokens"
  | "tool_call_count"
  | "commit_count"
  | "pull_request_count"
  | "lines_added"
  | "lines_removed";

const COLUMNS: { key: SortKey; label: string }[] = [
  { key: "started_at", label: "Started" },
  { key: "duration_ms", label: "Duration" },
  { key: "total_cost_usd", label: "Cost" },
  { key: "total_tokens", label: "Tokens" },
  { key: "tool_call_count", label: "Tool calls" },
  { key: "commit_count", label: "Commits" },
  { key: "pull_request_count", label: "PRs" },
  { key: "lines_added", label: "+lines" },
  { key: "lines_removed", label: "-lines" },
];

export default function Sessions() {
  const navigate = useNavigate();
  const [range, setRange] = useState<TimeRange>("30d");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [sortKey, setSortKey] = useState<SortKey>("started_at");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("desc");

  useEffect(() => {
    listSessions(200, 0, range)
      .then(setSessions)
      .catch((e) => setError(String(e)));
  }, [range]);

  const sorted = useMemo(() => {
    const rows = [...sessions];
    rows.sort((a, b) => {
      const av = a[sortKey] ?? 0;
      const bv = b[sortKey] ?? 0;
      return sortDir === "asc" ? av - bv : bv - av;
    });
    return rows;
  }, [sessions, sortKey, sortDir]);

  function toggleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  }

  return (
    <PageShell title="Sessions" subtitle="Every recorded Claude Code session — click a row to open its waterfall">
      <div className="mb-4 flex items-center justify-end">
        <TimeRangeSelect value={range} onChange={setRange} />
      </div>

      {error && <p className="mb-4 text-sm text-fault">{error}</p>}
      <div className="overflow-x-auto rounded border border-wire">
        <table className="w-full min-w-[900px] text-left text-sm">
          <thead>
            <tr className="border-b border-wire bg-panel">
              {COLUMNS.map((col) => (
                <th
                  key={col.key}
                  onClick={() => toggleSort(col.key)}
                  className="cursor-pointer select-none whitespace-nowrap px-3 py-2 text-[11px] uppercase tracking-wide text-ink/50 hover:text-ink"
                >
                  {col.label}
                  {sortKey === col.key && (sortDir === "asc" ? " ▲" : " ▼")}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.map((s) => (
              <tr
                key={s.session_id}
                onClick={() => navigate(`/waterfall?session=${s.session_id}`)}
                className="cursor-pointer border-b border-wire/40 hover:bg-wire/20"
              >
                <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-ink">
                  {formatTimestamp(s.started_at)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {formatDurationMs(s.duration_ms)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {formatCostUsd(s.total_cost_usd)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {(s.total_tokens ?? 0).toLocaleString()}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {s.tool_call_count ?? 0}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {s.commit_count ?? 0}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-ink/80">
                  {s.pull_request_count ?? 0}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-cat-subagent">
                  {s.lines_added ?? 0}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-fault">
                  {s.lines_removed ?? 0}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {sorted.length === 0 && (
          <div className="p-4 text-sm text-ink/50">No sessions recorded yet.</div>
        )}
      </div>
    </PageShell>
  );
}
