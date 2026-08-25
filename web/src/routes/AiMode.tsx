import { useEffect, useState } from "react";
import { PageShell } from "../components/PageShell";
import { Toolbar } from "../components/Toolbar";
import { TimeRangeSelect } from "../components/TimeRangeSelect";
import { type TimeRange } from "../lib/timeRange";
import { formatRelative } from "../lib/format";
import {
  getAiConfig,
  saveAiApiKey,
  clearAiApiKey,
  getCachedRecommendations,
  generateRecommendations,
  type AiRecommendation,
} from "../lib/apiAi";

const CATEGORY_LABEL: Record<AiRecommendation["category"], string> = {
  efficiency: "Efficiency",
  sessions: "Sessions",
  waste: "Waste",
  tools: "Tools & MCP",
};

function AiRecommendationCard({ rec }: { rec: AiRecommendation }) {
  const isWarn = rec.severity === "warn";
  return (
    <article className={`card flag ${isWarn ? "" : "flag-signal"} flex flex-col`}>
      <div className="flex-1 p-4.5">
        <span className={`pill ${isWarn ? "pill-fault" : "pill-signal"}`}>
          {CATEGORY_LABEL[rec.category]}
        </span>
        <div className="flag-title">{rec.title}</div>
        <p className="flag-text">{rec.detail}</p>
        {rec.evidence.length > 0 && (
          <div className="mt-3 flex flex-wrap gap-1.5">
            {rec.evidence.map((e, i) => (
              <span key={i} className="chip">
                {e.label}: {e.value}
              </span>
            ))}
          </div>
        )}
      </div>
    </article>
  );
}

export default function AiMode() {
  const [range, setRange] = useState<TimeRange>("30d");
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [keyInput, setKeyInput] = useState("");
  const [savingKey, setSavingKey] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [result, setResult] = useState<{
    generatedAt: number;
    fromCache: boolean;
    recommendations: AiRecommendation[];
    droppedCount: number;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getAiConfig()
      .then((c) => setConfigured(c.configured))
      .catch(() => setConfigured(false));
  }, []);

  useEffect(() => {
    getCachedRecommendations(range)
      .then((r) =>
        setResult({
          generatedAt: r.generated_at,
          fromCache: r.from_cache,
          recommendations: r.recommendations,
          droppedCount: r.dropped_count,
        }),
      )
      .catch(() => setResult(null));
  }, [range]);

  function handleSaveKey() {
    if (!keyInput.trim()) return;
    setSavingKey(true);
    setError(null);
    saveAiApiKey(keyInput.trim())
      .then((c) => {
        setConfigured(c.configured);
        setKeyInput("");
      })
      .catch((e) => setError(String(e)))
      .finally(() => setSavingKey(false));
  }

  function handleRemoveKey() {
    setSavingKey(true);
    setError(null);
    clearAiApiKey()
      .then((c) => setConfigured(c.configured))
      .catch((e) => setError(String(e)))
      .finally(() => setSavingKey(false));
  }

  function handleGenerate(force: boolean) {
    setGenerating(true);
    setError(null);
    generateRecommendations(range, force)
      .then((r) =>
        setResult({
          generatedAt: r.generated_at,
          fromCache: r.from_cache,
          recommendations: r.recommendations,
          droppedCount: r.dropped_count,
        }),
      )
      .catch((e) => setError(String(e)))
      .finally(() => setGenerating(false));
  }

  return (
    <PageShell
      title="AI Mode"
      subtitle="Claude Haiku reads this install's own pre-aggregated metrics and proposes recommendations — never raw prompt or tool content."
    >
      <Toolbar left={<TimeRangeSelect value={range} onChange={setRange} allowAllTime={false} />} />

      {error && <p className="text-sm text-fault">{error}</p>}

      <div className="card card-pad">
        <div className="card-title">Anthropic API key</div>
        {configured === null ? (
          <p className="card-desc mt-2">Checking…</p>
        ) : configured ? (
          <div className="mt-3 flex items-center justify-between gap-3">
            <p className="card-desc">A key is configured on this machine.</p>
            <button type="button" className="btn" disabled={savingKey} onClick={handleRemoveKey}>
              Remove key
            </button>
          </div>
        ) : (
          <div className="mt-3 flex flex-wrap items-end gap-3">
            <label className="flex flex-col gap-1.5">
              <span className="field-label">Anthropic API key</span>
              <input
                type="password"
                className="text-input w-[320px]"
                placeholder="sk-ant-..."
                value={keyInput}
                onChange={(e) => setKeyInput(e.target.value)}
              />
            </label>
            <button
              type="button"
              className="btn-primary"
              disabled={savingKey || !keyInput.trim()}
              onClick={handleSaveKey}
            >
              {savingKey ? "Saving…" : "Save key"}
            </button>
          </div>
        )}
        <p className="mt-3.5 text-[12.5px] leading-relaxed text-ink/45">
          ccotel makes zero network calls on its own — this is the one deliberate exception. Your
          key is stored only in <span className="code">~/.ccotel/config.json</span> on this
          machine and is sent only to api.anthropic.com, only when you click Generate. No prompt
          text, response text, tool input/output, file paths, or commands are ever sent — only
          pre-aggregated counts, costs, and durations already shown elsewhere in this dashboard.
        </p>
      </div>

      <div className="flex items-center gap-3">
        <button
          type="button"
          className="btn-primary"
          disabled={!configured || generating}
          onClick={() => handleGenerate(false)}
        >
          {generating ? "Generating…" : "Generate recommendations"}
        </button>
        {result && result.generatedAt > 0 && (
          <button type="button" className="btn" disabled={!configured || generating} onClick={() => handleGenerate(true)}>
            Force regenerate
          </button>
        )}
        {result && result.fromCache && result.generatedAt > 0 && (
          <span className="text-[12.5px] text-ink/45">
            Showing a cached result from {formatRelative(result.generatedAt)} — nothing was sent to
            Anthropic just now.
          </span>
        )}
      </div>

      {result && result.droppedCount > 0 && (
        <p className="text-[12.5px] text-ink/45">
          {result.droppedCount} proposed recommendation{result.droppedCount === 1 ? "" : "s"} could
          not be verified against this install's own data and were withheld.
        </p>
      )}

      {result && result.recommendations.length > 0 && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {result.recommendations.map((rec) => (
            <AiRecommendationCard key={rec.id} rec={rec} />
          ))}
        </div>
      )}

      {result && result.recommendations.length === 0 && (
        <div className="empty">
          {configured
            ? "No recommendations yet for this range — click Generate to ask Claude Haiku."
            : "Add an Anthropic API key above, then click Generate to get AI-authored recommendations."}
        </div>
      )}
    </PageShell>
  );
}
