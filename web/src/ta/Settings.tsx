import { useEffect, useState } from "react";
import { api, type AiConfig } from "./api";
import { Shell } from "./Shell";

// Settings holds the provider key for AI insights. The key is written once
// through the API and never read back, so the page only learns whether one is
// configured. Nothing here is sent anywhere until the user asks for an
// AI-backed insight.
export default function Settings() {
  const [ai, setAi] = useState<AiConfig | null>(null);
  const [key, setKey] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api.aiConfig().then(setAi).catch(() => {});
  }, []);

  const save = async (value: string) => {
    setSaving(true);
    setMessage(null);
    try {
      setAi(await api.saveAIKey(value));
      setKey("");
      setMessage(value ? "Key saved. It stays on this machine." : "Key removed.");
    } catch (e) {
      setMessage((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const choose = async (provider: string) => {
    setMessage(null);
    try {
      setAi(await api.chooseAIProvider(provider));
    } catch (e) {
      setMessage((e as Error).message);
    }
  };

  const configured = ai?.configured ?? null;

  return (
    <Shell>
      <div className="top-row">
        <p className="eb">Settings</p>
      </div>
      <section className="section">
        <div className="section-head">
          <h2 className="h2">AI insights</h2>
        </div>
        <article className="fcard">
          <strong className="big">{ai?.provider_name ?? "Provider"} API key</strong>
          <span className="body-copy">
            Everything in Token Advisor works without a key: every number and every fix is computed locally from your telemetry.
            A key is only used for explicit AI-backed insights, and nothing is sent until you ask for one.
          </span>
          <span className="body-copy">
            Status: <b>{configured === null ? "checking…" : configured ? "configured" : "not set"}</b>
          </span>
          {ai && ai.providers.length > 1 && (
            <>
              <label htmlFor="ai-provider" className="lbl">Provider</label>
              <select id="ai-provider" className="select" value={ai.provider} onChange={(e) => choose(e.target.value)}>
                {ai.providers.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </select>
            </>
          )}
          <form
            className="settings-form"
            onSubmit={(e) => {
              e.preventDefault();
              if (key.trim()) save(key.trim());
            }}
          >
            <label htmlFor="ai-key" className="lbl">Key</label>
            <input id="ai-key" type="password" autoComplete="off" placeholder={ai?.key_hint} value={key} onChange={(e) => setKey(e.target.value)} />
            <div className="actions">
              <button className="btn btn-k" type="submit" disabled={saving || !key.trim()}>Save key</button>
              {configured && (
                <button className="btn btn-q" type="button" disabled={saving} onClick={() => save("")}>Remove key</button>
              )}
            </div>
          </form>
          {message && <span className="hint-line" role="status">{message}</span>}
        </article>
      </section>
    </Shell>
  );
}
