import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { setFindingState, type Finding } from "../lib/api";
import { formatCostUsd, formatDurationMs, formatTokenCount } from "../lib/format";
import { MiniViz } from "./MiniViz";

const SEVERITY_FLAG_CLASS: Record<Finding["severity"], string> = {
  bad: "",
  warn: "flag-warn",
  good: "flag-signal",
  info: "flag-signal",
};

const FIX_LABEL: Record<Finding["fix_lang"], string> = {
  bash: "Run",
  "settings-json": "Add to ~/.claude/settings.json",
  "claude-md": "Add to CLAUDE.md",
};

function impactChips(impact: Finding["impact"]): string[] {
  const chips: string[] = [];
  if (impact.usd != null) chips.push(formatCostUsd(impact.usd));
  if (impact.tokens_est != null) {
    chips.push(`${impact.estimated ? "~" : ""}${formatTokenCount(impact.tokens_est)} tok`);
  }
  if (impact.minutes != null) chips.push(formatDurationMs(impact.minutes * 60000));
  return chips;
}

// InfoTooltip is a single (i) badge carrying a disclaimer/methodology note
// off to the side, on hover/focus — so an honesty caveat doesn't cost two
// lines of primary card space just to stay visible.
function InfoTooltip({ note }: { note: string }) {
  return (
    <span
      tabIndex={0}
      title={note}
      className="inline-flex h-4 w-4 shrink-0 cursor-help items-center justify-center rounded-full border border-wire-2 text-[10px] text-ink/45 hover:border-ink/40 hover:text-ink/70"
      aria-label={note}
    >
      i
    </span>
  );
}

// ActionMenu collapses Dismiss/Snooze behind one "⋯" control — Mark fixed
// is the one action worth a full button, since it's the one you take when
// you're actually done.
function ActionMenu({ onDismiss, onSnooze }: { onDismiss: () => void; onSnooze: () => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onClickAway(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onClickAway);
    return () => document.removeEventListener("mousedown", onClickAway);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        className="btn btn-icon"
        aria-label="More actions"
        onClick={() => setOpen((o) => !o)}
      >
        ⋯
      </button>
      {open && (
        <div className="absolute right-0 top-full z-10 mt-1 w-40 overflow-hidden rounded-md border border-wire-2 bg-panel shadow-lg">
          <button
            type="button"
            className="block w-full px-3 py-2 text-left text-[13px] text-ink/80 hover:bg-panel-2"
            onClick={() => {
              setOpen(false);
              onSnooze();
            }}
          >
            Snooze 7 days
          </button>
          <button
            type="button"
            className="block w-full px-3 py-2 text-left text-[13px] text-ink/80 hover:bg-panel-2"
            onClick={() => {
              setOpen(false);
              onDismiss();
            }}
          >
            Dismiss
          </button>
        </div>
      )}
    </div>
  );
}

// FindingCard renders one ranked, actionable card on the Fix page: a news-
// style title (never a category label), the evidence, its priced impact,
// an optional inline mini-viz, a copy-pasteable fix, and Mark fixed / a
// collapsed Dismiss-or-Snooze menu — each action posts to
// /api/findings/{id}/state and re-fetches via onChanged.
export function FindingCard({
  finding,
  onChanged,
}: {
  finding: Finding;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  async function act(state: "dismissed" | "snoozed" | "fixed") {
    setBusy(true);
    try {
      await setFindingState(finding.id, state, state === "snoozed" ? 7 : undefined);
      onChanged();
    } finally {
      setBusy(false);
    }
  }

  function copyFix() {
    navigator.clipboard
      ?.writeText(finding.fix)
      .then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      })
      .catch(() => {});
  }

  const chips = impactChips(finding.impact);

  return (
    <article className={`card flag ${SEVERITY_FLAG_CLASS[finding.severity]} flex flex-col`}>
      <div className="flex-1 p-4.5">
        <div className="flag-title">{finding.title}</div>
        <p className="flag-text">{finding.evidence}</p>

        {chips.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center gap-1.5">
            {chips.map((c) => (
              <span key={c} className="chip font-mono">
                {c}
              </span>
            ))}
            {finding.impact.note && <InfoTooltip note={finding.impact.note} />}
          </div>
        )}

        {finding.viz && (
          <div className="mt-3.5 h-[60px] overflow-hidden">
            <MiniViz viz={finding.viz} />
          </div>
        )}

        <div className="mt-3.5">
          <div className="mb-1 flex items-center justify-between">
            <span className="text-[11px] uppercase tracking-wide text-ink/45">
              {FIX_LABEL[finding.fix_lang]}
            </span>
            <button type="button" onClick={copyFix} className="text-[11px] text-signal hover:underline">
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <pre className="code block max-h-32 overflow-auto whitespace-pre-wrap py-2 leading-relaxed">
            {finding.fix}
          </pre>
        </div>
      </div>

      <div className="flex items-center justify-between gap-2 border-t border-wire px-4.5 py-3">
        {finding.link ? (
          <Link to={finding.link} className="text-[13px] text-signal hover:underline">
            View evidence →
          </Link>
        ) : (
          <span />
        )}
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            disabled={busy}
            className="btn border-good/40 text-good hover:bg-good/10"
            onClick={() => act("fixed")}
          >
            ✓ Fixed
          </button>
          <ActionMenu onDismiss={() => act("dismissed")} onSnooze={() => act("snoozed")} />
        </div>
      </div>
    </article>
  );
}
