// Package cli implements infrapal-token-advisor's subcommand dispatch. Each subcommand
// lives in its own file; this file only wires argv to the right handler.
package cli

import (
	"flag"
	"fmt"
)

const usage = `token-advisor — local Claude Code telemetry collector

Usage:
  token-advisor init            patch ~/.claude/settings.json, create DB, install background service
  token-advisor start           run collector and dashboard in foreground (what the service invokes)
  token-advisor stop            stop the background service
  token-advisor restart         restart the background service, e.g. after an upgrade
  token-advisor status          is the service up, DB size, event count, last event timestamp
  token-advisor ui              open the dashboard, serving it here if the service isn't running
  token-advisor mcp             serve captured metrics to Claude over MCP (stdio)
  token-advisor doctor          diagnose why no data is arriving
  token-advisor logs            show the collector's log (-n 100, --errors, -f)
  token-advisor export          export captured data (--format jsonl|csv --since 7d)
  token-advisor purge           delete captured data (--before 30d | --session <id> | --all)
  token-advisor update          install the latest release (--check only reports it)
  token-advisor uninstall       revert settings.json from backup, remove service
  token-advisor version         print the token-advisor version

Run "token-advisor <command> -h" for flags on a specific command.
`

// Version is infrapal-token-advisor's build version, overridden at release build time via
// -ldflags "-X .../internal/cli.Version=...". "dev" identifies a build made
// with plain `go build`/`go run` outside the goreleaser release process.
var Version = "dev"

// commands maps subcommand name to handler. Each handler receives argv with
// the subcommand itself already stripped off.
var commands = map[string]func(args []string) error{
	"init":      runInit,
	"start":     runStart,
	"stop":      runStop,
	"restart":   runRestart,
	"status":    runStatus,
	"ui":        runUI,
	"mcp":       runMCP,
	"doctor":    runDoctor,
	"logs":      runLogs,
	"export":    runExport,
	"purge":     runPurge,
	"update":    runUpdate,
	"uninstall": runUninstall,
}

// Run dispatches args (os.Args[1:]) to the matching subcommand.
func Run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "-v", "--version", "version":
		fmt.Println("token-advisor " + Version)
		return nil
	}

	handler, ok := commands[args[0]]
	if !ok {
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
	return handler(args[1:])
}

// newFlagSet builds a FlagSet that prints usage to stdout on -h, matching
// the rest of the CLI's tone rather than the flag package's default stderr
// "flag provided but not defined" style.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs
}
