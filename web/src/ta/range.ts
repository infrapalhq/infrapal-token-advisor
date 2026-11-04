import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";
import type { Range } from "./api";

const KEY = "ta-range";

// PERIODS is the one list of time ranges, in the order every page shows them.
export const PERIODS: { value: Range; label: string; short: string }[] = [
  { value: "today", label: "Today", short: "Today" },
  { value: "7d", label: "Last 7 days", short: "7 days" },
  { value: "30d", label: "Last 30 days", short: "30 days" },
  { value: "3m", label: "Last 3 months", short: "3 months" },
];

function isRange(v: string | null): v is Range {
  return PERIODS.some((p) => p.value === v);
}

function stored(): Range {
  try {
    const v = localStorage.getItem(KEY);
    return isRange(v) ? v : "7d";
  } catch {
    return "7d";
  }
}

// useRange is the time range the page shows. The address wins (so a link keeps
// working), then the last range chosen on any page, then 7 days. An address
// with a range we don't know falls back instead of showing an error.
export function useRange(): [Range, (r: Range) => void] {
  const [params, setParams] = useSearchParams();
  const fromUrl = params.get("range");
  const range = isRange(fromUrl) ? fromUrl : stored();
  const set = useCallback(
    (r: Range) => {
      try {
        localStorage.setItem(KEY, r);
      } catch {
        /* storage unavailable: the range still applies to this page */
      }
      setParams((p) => {
        const next = new URLSearchParams(p);
        next.set("range", r);
        return next;
      });
    },
    [setParams],
  );
  return [range, set];
}
