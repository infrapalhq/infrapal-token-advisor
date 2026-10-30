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
  token-advisor status          is the service up, DB size, event count, last event timestamp
  token-advisor ui              open the dashboard, serving it here if the service isn't running
  token-advisor doctor          diagnose why no data is arriving
  token-advisor export          export captured data (--format jsonl|csv --since 7d)
  token-advisor purge           delete captured data (--before 30d | --session <id> | --all)
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
	"status":    runStatus,
	"ui":        runUI,
	"doctor":    runDoctor,
	"export":    runExport,
	"purge":     runPurge,
	"uninstall": runUninstall,
	"replay":    runReplay, // hidden: not listed in usage, fixture-driven ingest testing
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

// notImplemented prints a stub message for commands whose real behavior
// arrives in a later build milestone, per the project's build order.
func notImplemented(name string, milestone string) error {
	fmt.Printf("token-advisor %s: not implemented yet — arrives in milestone %s\n", name, milestone)
	return nil
}

// newFlagSet builds a FlagSet that prints usage to stdout on -h, matching
// the rest of the CLI's tone rather than the flag package's default stderr
// "flag provided but not defined" style.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs
}
