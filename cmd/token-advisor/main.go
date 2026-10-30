// Command token-advisor is a local OTLP receiver that captures Claude Code's
// telemetry into SQLite and serves an embedded dashboard for exploring it.
package main

import (
	"fmt"
	"os"

	"github.com/infrapalhq/infrapal-token-advisor/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "token-advisor:", err)
		os.Exit(1)
	}
}
