import { useCallback, useEffect, useState } from "react";
import { api, type McpSetup } from "./api";
import { slashCommands } from "./mcp";

export function CopyBlock({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="mcp-copy">
      <pre className="code">{text}</pre>
      <button
        className="btn"
        type="button"
        onClick={() => {
          navigator.clipboard?.writeText(text).catch(() => {});
          setCopied(true);
        }}
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}

export function EnableMcp() {
  const [setup, setSetup] = useState<McpSetup | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);

  const load = useCallback(
    () =>
      api.mcpSetup()
        .then((s) => {
          setSetup(s);
          setError(null);
        })
        .catch((e: Error) => setError(e.message)),
    [],
  );

  useEffect(() => {
    load();
  }, [load]);

  const check = () => {
    setChecking(true);
    load().finally(() => setChecking(false));
  };

  if (error && !setup) return <p className="hint-line">Could not load MCP setup: {error}</p>;
  if (!setup) return null;

  return (
    <section className="card mcp" aria-label="Enable MCP">
      <div className="card-top">
        <strong>{setup.enabled ? "MCP is enabled" : `Enable MCP to ask ${setup.agent_name} about this data`}</strong>
        <span className={`ok${setup.enabled ? "" : " dashed"}`}>{setup.enabled ? "On" : "Off"}</span>
      </div>
      <span className="body-copy">
        Connect Infrapal Token Advisor to {setup.agent_name}, so it can look through this data and suggest fixes. It uses your {setup.agent_name} plan, so there's no API key to add.
      </span>
      {!setup.enabled && (
        <>
          <div className="mcp-step">
            <b>1. Paste this in your terminal</b>
            <CopyBlock text={setup.command} />
          </div>
          <div className="mcp-step">
            <b>2. Check that it shows as connected</b>
            <CopyBlock text={setup.verify} />
          </div>
        </>
      )}
      <div className="mcp-step">
        <b>{setup.enabled ? `Start a new ${setup.agent_name} session and run` : `3. Start a new ${setup.agent_name} session and run`}</b>
        <CopyBlock text={slashCommands(setup.server_name)[0].command} />
        <span className="hint-line">Or ask about your usage in your own words.</span>
      </div>
      {setup.enabled ? (
        <div className="mcp-step">
          <b>To turn it off</b>
          <CopyBlock text={setup.remove} />
        </div>
      ) : (
        <div className="acts">
          <button className="btn btn-k" type="button" disabled={checking} onClick={check}>{checking ? "Checking…" : "Check again"}</button>
        </div>
      )}
    </section>
  );
}
