# Roadmap

This is what we plan to work on over the next year. It's a plan, not a promise:
priorities change as we learn from people using it. To suggest something, open a
[feature request](https://github.com/infrapalhq/infrapal-token-advisor/issues/new?template=feature_request.yml)
or start a [discussion](https://github.com/infrapalhq/infrapal-token-advisor/discussions).

## Now: the first release

- **v0.1.0**, supporting Claude Code on macOS and Linux, installed with Homebrew or
  a script.
- **Signed releases** with build provenance, so you can check a download came from
  this repository's CI.
- **OpenSSF Best Practices Silver**, including higher test coverage and tests for
  the dashboard.

## Next: more agents

- **A second agent**, most likely OpenAI's Codex CLI, built on the adapter
  interface. This is what proves the interface, which stays experimental until then.
- **Gemini CLI** support, and other agents as contributors add them. See
  [adding an agent](docs/ADDING_AN_AGENT.md).
- **More checks**, such as large tool results and unused context, using the
  [detector guide](docs/ADDING_A_DETECTOR.md).
- **Security headers** on the dashboard.

## Later

- **Signed and notarized macOS binaries**, so Gatekeeper doesn't warn on first run.
- **OpenSSF Best Practices Gold**, which needs outside maintainers and higher test
  coverage.
- **A Windows build**, if there's demand.
- **Comparing periods**, such as this week against last, to see whether a change
  helped.

## Not planned

These would change what token-advisor is, so we don't plan to do them:

- **A hosted or cloud version.** Your data stays on your machine.
- **Accounts, logins or usage telemetry** about you.
- **A proxy** between your agent and its API. token-advisor only reads what the
  agent already reports.
- **Changing your agent's behaviour automatically.** Every fix is something you
  choose to paste in.
