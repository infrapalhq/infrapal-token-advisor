package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/infrapalhq/infrapal-token-advisor/internal/mcpserver"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// runMCP serves the MCP tools over stdio until the client closes stdin. Claude
// Code launches this itself; stdout carries the protocol, so nothing else may
// print there.
func runMCP(args []string) error {
	fs := newFlagSet("mcp")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Create or migrate through the normal path first, so the views exist even
	// if this binary is newer than the running collector.
	migrated, p, err := openDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	migrated.Close()

	db, err := store.OpenReadOnly(p.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	return mcpserver.New(db, Version).Serve(context.Background(), os.Stdin, os.Stdout)
}
