import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { type TimeRange } from "./timeRange";

const STORAGE_KEY = "infrapal:range";

function initialRange(): TimeRange {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "7d" || stored === "30d" || stored === "90d" || stored === "") {
    return stored;
  }
  return "30d";
}

type DateRangeContextValue = {
  range: TimeRange;
  setRange: (range: TimeRange) => void;
};

const DateRangeContext = createContext<DateRangeContextValue | null>(null);

// DateRangeProvider holds the one date-range picker's value for the whole
// app (mirrors lib/theme.ts's persist-to-localStorage pattern) — every page
// reads/writes the same range instead of keeping its own.
export function DateRangeProvider({ children }: { children: ReactNode }) {
  const [range, setRange] = useState<TimeRange>(initialRange);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, range);
  }, [range]);

  return (
    <DateRangeContext.Provider value={{ range, setRange }}>
      {children}
    </DateRangeContext.Provider>
  );
}

export function useDateRange(): DateRangeContextValue {
  const ctx = useContext(DateRangeContext);
  if (!ctx) {
    throw new Error("useDateRange must be used within a DateRangeProvider");
  }
  return ctx;
}
