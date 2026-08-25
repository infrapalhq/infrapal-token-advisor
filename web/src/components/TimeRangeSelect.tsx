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
    <label className="flex flex-col gap-1.5">
      <span className="field-label">Date range</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value as TimeRange)}
        className="select w-[210px]"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  );
}
