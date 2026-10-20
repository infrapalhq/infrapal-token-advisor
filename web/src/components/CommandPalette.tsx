import { useEffect, useRef, useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { searchEvents, type SearchHit } from "../lib/api";
import { formatTimestamp } from "../lib/format";

export const OPEN_COMMAND_PALETTE_EVENT = "infrapal:open-command-palette";

const STATIC_TARGETS: { label: string; hint: string; to: string }[] = [
  { label: "Fix", hint: "Ranked findings and the recoverable-tokens trend", to: "/" },
  { label: "Spend", hint: "Token composition, cost concentration, tools & models", to: "/spend" },
  { label: "Trace", hint: "Every recorded conversation, browsable and searchable", to: "/trace" },
];

// renderSnippet renders an FTS5 snippet() result. The <mark>/</mark>
// delimiters are ones *we* asked SQLite to insert (see
// internal/api/search.go) — everything else in the string is raw
// prompt/response/command content the user typed, which may itself
// contain literal "<...>" text (pasted HTML, code fragments). Splitting on
// the exact delimiter strings and rendering the remainder as plain React
// text (not dangerouslySetInnerHTML) keeps that content inert instead of
// re-parsing it as markup.
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

// CommandPalette is the app's single global ⌘K overlay: static navigation
// targets plus a live full-text search over prompts/responses/commands.
// Mounted once in App.tsx; opens on Cmd/Ctrl+K from anywhere, or when
// something dispatches OPEN_COMMAND_PALETTE_EVENT (Topbar's "Jump to"
// button).
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchHit[]>([]);
  const [searching, setSearching] = useState(false);
  const navigate = useNavigate();
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      } else if (e.key === "Escape") {
        setOpen(false);
      }
    }
    function onOpenEvent() {
      setOpen(true);
    }
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener(OPEN_COMMAND_PALETTE_EVENT, onOpenEvent);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener(OPEN_COMMAND_PALETTE_EVENT, onOpenEvent);
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setResults([]);
    const raf = requestAnimationFrame(() => inputRef.current?.focus());
    return () => cancelAnimationFrame(raf);
  }, [open]);

  useEffect(() => {
    const trimmed = query.trim();
    if (!trimmed) {
      setResults([]);
      setSearching(false);
      return;
    }
    setSearching(true);
    const handle = setTimeout(() => {
      searchEvents(trimmed)
        .then(setResults)
        .catch(() => setResults([]))
        .finally(() => setSearching(false));
    }, 150);
    return () => clearTimeout(handle);
  }, [query]);

  if (!open) return null;

  const trimmed = query.trim();
  const filteredTargets = STATIC_TARGETS.filter((t) =>
    t.label.toLowerCase().includes(trimmed.toLowerCase()),
  );

  function go(to: string) {
    setOpen(false);
    navigate(to);
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-void/60 pt-[12vh]"
      onClick={() => setOpen(false)}
    >
      <div
        className="w-full max-w-xl overflow-hidden rounded-lg border border-wire-2 bg-panel shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2.5 border-b border-wire px-4 py-3">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--color-ink)" strokeOpacity={0.45} strokeWidth="1.8" strokeLinecap="round">
            <circle cx="11" cy="11" r="7" />
            <path d="M21 21l-4.3-4.3" />
          </svg>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Jump to a page, or search prompts, tools, sessions…"
            className="flex-1 border-0 bg-transparent font-body text-sm text-ink outline-none"
          />
          <span className="font-mono text-xs text-ink/45">Esc</span>
        </div>

        <div className="max-h-96 overflow-y-auto p-2">
          {filteredTargets.length > 0 && (
            <div className="mb-1 flex flex-col">
              {filteredTargets.map((t) => (
                <button
                  key={t.to}
                  type="button"
                  onClick={() => go(t.to)}
                  className="flex flex-col rounded-md px-3 py-2 text-left hover:bg-panel-2"
                >
                  <span className="text-sm font-medium text-ink">{t.label}</span>
                  <span className="text-[12px] text-ink/50">{t.hint}</span>
                </button>
              ))}
            </div>
          )}

          {searching && <div className="px-3 py-2 text-[13px] text-ink/50">Searching…</div>}

          {results.map((r) => (
            <button
              key={`${r.kind}-${r.id}`}
              type="button"
              onClick={() =>
                go(
                  r.kind === "prompt"
                    ? `/trace?session=${r.session_id}&prompt=${r.id}`
                    : `/trace?session=${r.session_id}`,
                )
              }
              className="flex w-full flex-col rounded-md px-3 py-2 text-left hover:bg-panel-2"
            >
              <div className="mb-1 flex items-center justify-between">
                <span className={`pill ${r.kind === "prompt" ? "pill-signal" : "pill-warn"}`}>
                  {r.kind}
                </span>
                <span className="font-mono text-[10px] text-ink/40">{formatTimestamp(r.ts)}</span>
              </div>
              <p className="text-sm text-ink/90">{renderSnippet(r.snippet)}</p>
            </button>
          ))}

          {trimmed && !searching && results.length === 0 && filteredTargets.length === 0 && (
            <div className="empty">No matches for "{trimmed}".</div>
          )}
          {!trimmed && (
            <div className="px-3 py-2 text-[12.5px] text-ink/40">
              Type to search prompts, tool calls and sessions.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
