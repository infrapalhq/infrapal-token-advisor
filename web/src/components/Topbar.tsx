import type { Theme } from "../lib/theme";
import { useDateRange } from "../lib/dateRange";
import { TimeRangeSelect } from "./TimeRangeSelect";
import { OPEN_COMMAND_PALETTE_EVENT } from "./CommandPalette";

const iconProps = {
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export function Topbar({
  theme,
  onToggleTheme,
}: {
  theme: Theme;
  onToggleTheme: () => void;
}) {
  const { range, setRange } = useDateRange();

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-wire bg-chrome px-5">
      <div className="flex items-center gap-3.5">
        <span className="flex items-center gap-2 font-display text-[17px] font-semibold tracking-tight text-ink">
          <svg width="18" height="18" viewBox="0 0 24 24" {...iconProps} stroke="var(--color-signal)">
            <path d="M6 17h11a4 4 0 0 0 0-8 6 6 0 0 0-11.5 2A3.5 3.5 0 0 0 6 17z" />
          </svg>
          infrapal
        </span>
        <span className="h-[18px] w-px bg-wire-2" />
        <span className="text-[13px] text-ink/55">Token Advisor</span>
      </div>
      <div className="flex items-center gap-3">
        <TimeRangeSelect value={range} onChange={setRange} compact />
        <span className="h-[18px] w-px bg-wire-2" />
        <button
          type="button"
          className="btn"
          onClick={() => window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT))}
        >
          <span className="font-mono text-xs text-ink/45">⌘K</span> Jump to
        </button>
        <button
          type="button"
          className="btn btn-icon"
          aria-label="Toggle theme"
          onClick={onToggleTheme}
        >
          {theme === "dark" ? (
            <svg width="15" height="15" viewBox="0 0 24 24" {...iconProps}>
              <circle cx="12" cy="12" r="4" />
              <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
            </svg>
          ) : (
            <svg width="15" height="15" viewBox="0 0 24 24" {...iconProps}>
              <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
            </svg>
          )}
        </button>
        <button type="button" className="btn" onClick={() => window.print()}>
          <svg width="14" height="14" viewBox="0 0 24 24" {...iconProps}>
            <path d="M12 3v12M7 11l5 5 5-5M4 20h16" />
          </svg>
          Export
        </button>
      </div>
    </header>
  );
}
