import { useEffect, useState } from "react";

// useRefresh returns a number that goes up every so often. Put it in an
// effect's dependencies to reload data quietly while a page stays open.
export function useRefresh(ms = 60_000): number {
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setTick((n) => n + 1), ms);
    return () => clearInterval(t);
  }, [ms]);
  return tick;
}
