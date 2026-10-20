import { TIME_RANGE_OPTIONS, type TimeRange } from "../lib/timeRange";

export function TimeRangeSelect({
  value,
  onChange,
  allowAllTime = true,
  compact = false,
}: {
  value: TimeRange;
  onChange: (value: TimeRange) => void;
  allowAllTime?: boolean;
  /** Skips the "Date range" label — for the single global picker in Topbar,
   * where the header itself supplies the context. */
  compact?: boolean;
}) {
  const options = allowAllTime
    ? TIME_RANGE_OPTIONS
    : TIME_RANGE_OPTIONS.filter((o) => o.value !== "");
  const select = (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value as TimeRange)}
      className={compact ? "select h-8 w-[150px] text-[13px]" : "select w-[210px]"}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );

  if (compact) return select;

  return (
    <label className="flex flex-col gap-1.5">
      <span className="field-label">Date range</span>
      {select}
    </label>
  );
}
