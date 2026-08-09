import { TIME_RANGE_OPTIONS, type TimeRange } from "../lib/timeRange";

export function TimeRangeSelect({
  value,
  onChange,
  allowAllTime = true,
}: {
  value: TimeRange;
  onChange: (value: TimeRange) => void;
  allowAllTime?: boolean;
}) {
  const options = allowAllTime
    ? TIME_RANGE_OPTIONS
    : TIME_RANGE_OPTIONS.filter((o) => o.value !== "");
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value as TimeRange)}
      className="rounded border border-wire bg-panel px-2 py-1.5 text-sm text-ink"
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
