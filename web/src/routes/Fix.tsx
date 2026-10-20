import { useCallback, useEffect, useMemo, useState } from "react";
import { PageShell } from "../components/PageShell";
import { Toolbar, ExportReportButton } from "../components/Toolbar";
import { FindingCard } from "../components/FindingCard";
import {
  getFindings,
  getFindingsActivity,
  type Finding,
  type FindingsActivity,
} from "../lib/api";
import { formatCostUsd } from "../lib/format";

const SEVERITY_RANK: Record<Finding["severity"], number> = { bad: 3, warn: 2, info: 1, good: 0 };

export default function Fix() {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [activity, setActivity] = useState<FindingsActivity | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  const load = useCallback(() => {
    getFindings()
      .then((all) => {
        setFindings(all.filter((f) => f.state === "open"));
        setLoaded(true);
      })
      .catch((e) => setError(String(e)));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    getFindingsActivity("7d")
      .then(setActivity)
      .catch(() => setActivity(null));
  }, [findings.length]);

  const priced = useMemo(
    () =>
      [...findings]
        .filter((f) => f.impact.usd != null)
        .sort((a, b) => (b.impact.usd ?? 0) - (a.impact.usd ?? 0)),
    [findings],
  );
  const free = useMemo(
    () =>
      [...findings]
        .filter((f) => f.impact.usd == null)
        .sort((a, b) => SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity]),
    [findings],
  );

  const totalUSD = useMemo(() => priced.reduce((sum, f) => sum + (f.impact.usd ?? 0), 0), [priced]);
  const topN = Math.min(2, priced.length);
  const topNUSD = useMemo(
    () => priced.slice(0, topN).reduce((sum, f) => sum + (f.impact.usd ?? 0), 0),
    [priced, topN],
  );

  return (
    <PageShell title="Fix" subtitle="Every card here can be fixed with a command or a config diff — nothing else qualifies.">
      <Toolbar right={<ExportReportButton />} />

      {error && <p className="text-sm text-fault">{error}</p>}

      {loaded && (
        <div>
          {priced.length > 0 ? (
            <h2 className="font-display text-[22px] font-semibold leading-snug text-ink">
              You're wasting {formatCostUsd(totalUSD)}.{" "}
              {topN > 0 && (
                <>
                  Fix the top {topN} and get {formatCostUsd(topNUSD)} back.
                </>
              )}
            </h2>
          ) : findings.length > 0 ? (
            <h2 className="font-display text-[22px] font-semibold leading-snug text-ink">
              Nothing's costing you money right now — {free.length} free fix{free.length === 1 ? "" : "es"} below.
            </h2>
          ) : (
            <h2 className="font-display text-[22px] font-semibold leading-snug text-ink">
              Nothing flagged. This install looks healthy.
            </h2>
          )}
          {activity && (activity.fixed > 0 || activity.dismissed > 0) && (
            <p className="mt-1.5 text-[13px] text-ink/50">
              {activity.fixed} fixed · {activity.dismissed} dismissed this week
            </p>
          )}
        </div>
      )}

      {priced.length > 0 && (
        <div className="flex flex-col gap-4">
          {free.length > 0 && <div className="text-[11px] uppercase tracking-wide text-ink/40">Costing you money</div>}
          {priced.map((f) => (
            <FindingCard key={f.id} finding={f} onChanged={load} />
          ))}
        </div>
      )}

      {free.length > 0 && (
        <div className="flex flex-col gap-4">
          {priced.length > 0 && <div className="text-[11px] uppercase tracking-wide text-ink/40">Free fixes</div>}
          {free.map((f) => (
            <FindingCard key={f.id} finding={f} onChanged={load} />
          ))}
        </div>
      )}

    </PageShell>
  );
}
