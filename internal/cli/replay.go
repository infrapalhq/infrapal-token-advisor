package cli

import "fmt"

// runReplay is hidden from top-level usage: it exists so the UI can be
// developed against fixture OTLP payloads without generating live traffic.
func runReplay(args []string) error {
	fs := newFlagSet("replay")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: ccotel replay <dir>")
	}
	return notImplemented("replay", "2/3 (ingest pipeline)")
}
