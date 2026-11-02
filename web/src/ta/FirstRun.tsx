import { useAgents } from "./agentContext";

// FirstRun stands in for a page of zeros until the first telemetry arrives.
export function FirstRun() {
  const { defaultName: name } = useAgents();
  return (
    <div className="first-run-page">
      <p className="eb">Getting started</p>
      <section className="hero">
        <h1>Waiting for your first session.</h1>
        <p className="lead">Nothing has arrived from {name} yet. This page updates on its own once it does.</p>
      </section>
      <ol className="first-run">
        <li>
          <strong>Connect {name}</strong>
          <span>Run <code>token-advisor init</code> if you haven't already.</span>
        </li>
        <li>
          <strong>Restart {name}</strong>
          <span>Sessions that are already open won't pick up the new settings.</span>
        </li>
        <li>
          <strong>Use it as normal</strong>
          <span>Your data stays on this machine.</span>
        </li>
      </ol>
      <p className="hint-line">Still nothing after a few minutes? Run <code>token-advisor doctor</code> to see what's wrong.</p>
    </div>
  );
}
