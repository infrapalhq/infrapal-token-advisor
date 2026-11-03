package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// untilInterrupted returns a context that ends on Ctrl+C or SIGTERM, which is
// how start, ui and logs -f know to stop. It's a variable so tests can stop them.
var untilInterrupted = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
