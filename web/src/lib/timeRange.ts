export type TimeRange = "7d" | "30d" | "90d" | "";

export const TIME_RANGE_OPTIONS: { value: TimeRange; label: string }[] = [
  { value: "7d", label: "1 week" },
  { value: "30d", label: "1 month" },
  { value: "90d", label: "3 months" },
  { value: "", label: "All time" },
];

export function timeRangeLabel(value: TimeRange): string {
  return TIME_RANGE_OPTIONS.find((o) => o.value === value)?.label ?? value;
}
