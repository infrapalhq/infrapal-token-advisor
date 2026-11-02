import { useEffect, useState } from "react";
import { api, type Status } from "./api";

export function useStatus(): Status | null {
  const [s, setS] = useState<Status | null>(null);
  useEffect(() => {
    let live = true;
    const load = () => api.status().then((v) => live && setS(v)).catch(() => {});
    load();
    const t = setInterval(load, 30_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, []);
  return s;
}

// useFirstRun is true until the collector has received anything at all, which
// tells a new install apart from a quiet date range.
export function useFirstRun(): boolean {
  const status = useStatus();
  return status !== null && status.last_event_ms === undefined;
}
