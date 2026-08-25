import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { PageShell } from "../components/PageShell";
import { searchEvents, type SearchHit } from "../lib/api";
import { formatTimestamp } from "../lib/format";

// Renders an FTS5 snippet() result. The <mark>/</mark> delimiters are ones
// *we* asked SQLite to insert (see internal/api/search.go) — everything
// else in the string is raw prompt/response/command content the user typed,
// which may itself contain literal "<...>" text (pasted HTML, code
// fragments). Splitting on the exact delimiter strings and rendering the
// remainder as plain React text (not dangerouslySetInnerHTML) keeps that
// content inert instead of re-parsing it as markup.
function renderSnippet(snippet: string): ReactNode[] {
  const parts = snippet.split(/(<mark>|<\/mark>)/);
  let marking = false;
  const out: ReactNode[] = [];
  parts.forEach((part, idx) => {
    if (part === "<mark>") {
      marking = true;
      return;
    }
    if (part === "</mark>") {
      marking = false;
      return;
    }
    if (!part) return;
    out.push(
      marking ? (
        <mark key={idx} className="rounded-sm bg-signal/25 text-signal">
          {part}
        </mark>
      ) : (
        <span key={idx}>{part}</span>
      ),
    );
  });
  return out;
}

export default function Search() {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchHit[]>([]);
  const [status, setStatus] = useState<"idle" | "loading" | "done">("idle");
  const [error, setError] = useState<string | null>(null);

  function runSearch(q: string) {
    const trimmed = q.trim();
    if (!trimmed) {
      setResults([]);
      setStatus("idle");
      return;
    }
    setStatus("loading");
    setError(null);
    searchEvents(trimmed)
      .then((r) => {
        setResults(r);
        setStatus("done");
      })
      .catch((e) => {
        setError(String(e));
        setStatus("done");
      });
  }

  return (
    <PageShell
      title="Search"
      subtitle="Find a prompt, a tool call or a session by anything recorded on it."
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          runSearch(query);
        }}
      >
        <div className="flex h-12 items-center gap-2.5 rounded-lg border border-wire-2 bg-panel px-4">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--color-ink)" strokeOpacity={0.45} strokeWidth="1.8" strokeLinecap="round">
            <circle cx="11" cy="11" r="7" />
            <path d="M21 21l-4.3-4.3" />
          </svg>
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search prompts, tools, sessions…"
            className="flex-1 border-0 bg-transparent font-body text-sm text-ink outline-none"
            autoFocus
          />
          <span className="font-mono text-xs text-ink/45">⌘K</span>
        </div>
      </form>

      {error && <p className="text-sm text-fault">{error}</p>}
      {status === "loading" && <p className="text-sm text-ink/55">Searching…</p>}
      {status === "done" && results.length === 0 && !error && (
        <div className="empty">No matches for "{query}".</div>
      )}
      {status === "idle" && (
        <div className="empty">Results appear here as you type.</div>
      )}

      <div className="flex flex-col gap-2">
        {results.map((r) => (
          <Link
            key={`${r.kind}-${r.id}`}
            to={
              r.kind === "prompt"
                ? `/waterfall?session=${r.session_id}&prompt=${r.id}`
                : `/waterfall?session=${r.session_id}`
            }
            className="card block p-3.5 hover:bg-panel-2"
          >
            <div className="mb-1.5 flex items-center justify-between">
              <span className={`pill ${r.kind === "prompt" ? "pill-signal" : "pill-warn"}`}>
                {r.kind}
              </span>
              <span className="font-mono text-[10px] text-ink/40">{formatTimestamp(r.ts)}</span>
            </div>
            <p className="text-sm text-ink/90">{renderSnippet(r.snippet)}</p>
          </Link>
        ))}
      </div>
    </PageShell>
  );
}
