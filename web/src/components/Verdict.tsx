export type VerdictTone = "good" | "warn" | "bad" | "neutral";

const TONE_CLASS: Record<VerdictTone, string> = {
  good: "text-good",
  warn: "text-warn",
  bad: "text-fault",
  neutral: "text-ink",
};

// Verdict is a header-strip stat: a value, a good/warn/bad/neutral colour,
// and (per the "every number carries a so-what" rule) a one-line note
// explaining what the number means, never a bare figure.
export function Verdict({
  label,
  value,
  tone = "neutral",
  note,
}: {
  label: string;
  value: string;
  tone?: VerdictTone;
  note?: string;
}) {
  return (
    <div className="card p-4.5">
      <div className="kpi-label">{label}</div>
      <div className={`kpi-value ${TONE_CLASS[tone]}`}>{value}</div>
      {note && <div className="kpi-note">{note}</div>}
    </div>
  );
}
