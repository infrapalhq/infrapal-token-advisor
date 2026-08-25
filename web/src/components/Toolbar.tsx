import type { ReactNode } from "react";

export function Toolbar({
  left,
  right,
}: {
  left?: ReactNode;
  right?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div className="flex flex-wrap items-end gap-3.5">{left}</div>
      {right && <div className="flex items-center gap-2">{right}</div>}
    </div>
  );
}

export function ExportReportButton() {
  return (
    <button type="button" className="btn-primary flex items-center gap-2" onClick={() => window.print()}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
        <path d="M12 3v12M7 11l5 5 5-5M4 20h16" />
      </svg>
      Export report
    </button>
  );
}
