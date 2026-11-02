# Adding an agent

token-advisor is built so you can add a coding agent without changing the core:
one new package, one line in a list, and tests that tell you when you're done.

The adapter interface is experimental. Claude Code is the only real agent so far,
so expect it to change as the next one teaches us something. If your agent doesn't
fit, tell us in your issue: that's useful, and the interface can move.

Before you start, [open a new agent issue](https://github.com/infrapalhq/infrapal-token-advisor/issues/new/choose),
so nobody duplicates your work.

## 1. Find out what your agent really reports

Start with real data, not its docs. Docs and telemetry often disagree, and the
contract tests can only check what you show them.

| Your agent | You'll implement |
| --- | --- |
| Exports OpenTelemetry logs | `agent.LogSource` |
| Keeps local files or a database | `agent.Importer` |
| Does both | Whichever gives better data, or both |

The collector takes OTLP over HTTP with protobuf only, on port 4318. If your agent
defaults to gRPC, setup needs to switch it to HTTP. Only logs reach an adapter for
now, so an agent that exports only metrics or traces needs an `Importer`.

To see what an agent sends, run a throwaway collector on spare ports, so your real
data stays separate:

```bash
mkdir -p /tmp/ta-capture/.infrapal-token-advisor
```

```bash
echo '{"otlp_port":14318,"api_port":14319,"retention_months":0}' > /tmp/ta-capture/.infrapal-token-advisor/config.json
```

```bash
HOME=/tmp/ta-capture token-advisor start
```

Point your agent at `http://localhost:14318`, use it for a while, then read what
arrived. Every record is kept in `events_raw`, with its full body:

```bash
sqlite3 /tmp/ta-capture/.infrapal-token-advisor/token-advisor.db "SELECT event_name, body FROM events_raw LIMIT 20"
```

Note the `service.name` in `body`, the field holding the session id, and how it
reports tokens, cost and tools.

## 2. Copy the example

[`internal/agent/example`](../internal/agent/example/example.go) is a complete
adapter for an imaginary agent, and it reports both ways. Copy it, and delete
whichever half you don't need:

```bash
cp -r internal/agent/example internal/agent/myagent
```

Rename the package and set `ID`. The ID is stored in the database, so it has to be
lowercase words with dashes, and it can't change later.

## 3. Add it to the list

Add one line to [`internal/agent/all/all.go`](../internal/agent/all/all.go):

```go
func Agents(home string) []agent.Agent {
	return []agent.Agent{
		claudecode.New(home),
		myagent.New(home),
	}
}
```

That's what connects it. `init --agent`, `doctor`, `status`, ingest, the detectors
and the dashboard all find it from this list, and it now runs through the basic
contract too. Keep Claude Code first, because the first entry is the default.

## 4. Say what it can do

`Capabilities` is a promise. List only what your agent really reports. A check that
needs something else is skipped for your agent, and the dashboard says so, which is
much better than reporting "no problems" from data it never had. It's fine to start
with few of these.

| Capability | Means | Without it |
| --- | --- | --- |
| `CapFilePaths` | Tool events carry the file | Re-read files aren't checked |
| `CapCommands` | Tool events carry the shell command | Repeated commands aren't checked |
| `CapApprovalWait` | Tool events carry how long the user took to approve | Repeated approvals aren't checked |
| `CapCompaction` | You send `Compaction` events | Compactions aren't checked |
| `CapMCPConnections` | You send `MCPConnection` events | Unused MCP servers aren't checked |
| `CapContextSize` | Request tokens show how full the context was | Nothing yet |
| `CapPromptText` | Prompt and response text can be captured | Nothing yet |

`CostSource` says where dollar figures come from:

| Value | Means |
| --- | --- |
| `CostPerRequest` | The agent reports each request's cost |
| `CostPerTurn` | The agent reports each prompt's total. Send `TurnCost` events |
| `CostComputed` | You work it out from tokens and prices |
| `CostNone` | No figures. The dashboard shows `—`, never `$0.00` |

## 5. Map its data to events

Everything downstream reads the events in
[`internal/agent/model.go`](../internal/agent/model.go). A few rules matter:

- **Never assume an attribute exists or has the right type.** It comes from outside
  the program. A panic here would stop ingest.
- **Return nothing for a record you don't recognise.** It's already stored raw, and
  new agent versions add new records all the time.
- **`Tokens.Input` never includes cached tokens.** If your agent's figure does, use
  `agent.InputWithoutCached`. `Output` includes reasoning.
- **A nil `CostMicros` means unknown, not free.**
- **Set `Meta` on every event.** An event with no session is dropped.
- **Tool calls can arrive in parts.** Use the same `CallID`, and leave anything you
  don't know yet empty.
- **`ToolCategory` must return `ToolOther` for anything you don't recognise.**
  Checks use categories to decide what's safe.
- **Importers return a bounded batch and a cursor.** Session files can be huge.

You never write SQL. `internal/ingest` stores events in the existing tables.

## 6. Connect, disconnect, check

`Connect` turns telemetry on, `Disconnect` undoes exactly that, and `Check` powers
`doctor`. Running `Connect` twice has to be safe, and it mustn't touch the user's
other settings. Back up a file before you change it, and only if you're changing it.

## 7. Optional: fixes and MCP

An agent with no `Advisor` still gets every finding, just with no fix to paste. Add
one when you have advice in your agent's own terms. Embed `agent.NoAdvice`, and
override only what you have, as the example does.

If your agent can use MCP servers, implement `agent.MCPHost`.

## 8. Test it

Being in the list gives you the basic contract. For the rest, add this to your
package, with records captured from a real session in step 1:

```go
func TestContract(t *testing.T) {
	agenttest.Run(t, agenttest.Suite{
		New:     func(home string) agent.Agent { return New(home) },
		Tools:   map[string]agent.ToolCategory{"read_file": agent.ToolRead},
		Samples: []agent.LogRecord{{Name: "tool_call", Ts: 1000, Attrs: map[string]any{}}},
	})
}
```

```bash
go test ./internal/agent/...
```

The contract checks identity, setup, bad input, cost and capability claims, and
that a resumed import doesn't repeat anything. Then try the real thing:

```bash
go build ./cmd/token-advisor
```

```bash
./token-advisor init --agent myagent
```

Use your agent, then open the dashboard with `./token-advisor ui`.

## What you don't need to touch

If you find yourself editing any of these, stop and tell us in your issue. It
probably means the interface is missing something.

- `internal/ingest`, `internal/store` or any SQL
- The detectors in `internal/api`
- The dashboard in `web/`
- `internal/agent/claudecode`

## Before you open a pull request

- [ ] `go test -race ./...` and `golangci-lint run` pass
- [ ] `TestContract` uses samples from a real session, with private data removed
- [ ] `Capabilities` and `CostSource` match what the agent really reports
- [ ] You tried `init`, `doctor`, the dashboard and `uninstall --agent` for real
- [ ] You added the agent's setup to the README
