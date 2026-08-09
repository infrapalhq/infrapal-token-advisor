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
        <mark key={idx} className="rounded-sm bg-signal/30 text-signal">
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
      subtitle="Full-text search across your prompt and response history"
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          runSearch(query);
        }}
        className="mb-4 flex gap-2"
      >
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search prompts and responses…"
          className="w-full max-w-md rounded border border-wire bg-panel px-3 py-1.5 text-sm text-ink outline-none focus-visible:ring-2 focus-visible:ring-signal"
          autoFocus
        />
        <button
          type="submit"
          className="rounded border border-wire bg-panel px-3 py-1.5 text-sm text-ink hover:bg-wire/30"
        >
          Search
        </button>
      </form>

      {error && <p className="mb-4 text-sm text-fault">{error}</p>}
      {status === "loading" && (
        <p className="text-sm text-ink/50">Searching…</p>
      )}
      {status === "done" && results.length === 0 && !error && (
        <p className="text-sm text-ink/50">No matches for "{query}".</p>
      )}

      <div className="space-y-2">
        {results.map((r) => (
          <Link
            key={`${r.kind}-${r.id}`}
            to={
              r.kind === "prompt"
                ? `/waterfall?session=${r.session_id}&prompt=${r.id}`
                : `/waterfall?session=${r.session_id}`
            }
            className="block rounded border border-wire bg-panel p-3 hover:bg-wire/20"
          >
            <div className="mb-1 flex items-center justify-between">
              <span
                className={[
                  "rounded-sm px-1.5 py-0.5 text-[10px] uppercase tracking-wide",
                  r.kind === "prompt"
                    ? "bg-cat-tool/20 text-cat-tool"
                    : "bg-cat-llm/20 text-cat-llm",
                ].join(" ")}
              >
                {r.kind}
              </span>
              <span className="font-mono text-[10px] text-ink/40">
                {formatTimestamp(r.ts)}
              </span>
            </div>
            <p className="text-sm text-ink/90">{renderSnippet(r.snippet)}</p>
          </Link>
        ))}
      </div>
    </PageShell>
  );
}
