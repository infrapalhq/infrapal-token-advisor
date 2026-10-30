package cli

import (
	"database/sql"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// openDB resolves infrapal-token-advisor's paths, ensures the base directory exists, and
// opens (creating + migrating if necessary) the SQLite database. Shared by
// every subcommand that needs the DB.
func openDB() (*sql.DB, paths.Paths, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, paths.Paths{}, err
	}
	if err := p.EnsureBaseDir(); err != nil {
		return nil, paths.Paths{}, err
	}
	db, err := openDBAt(p)
	if err != nil {
		return nil, paths.Paths{}, err
	}
	return db, p, nil
}

// openDBAt opens (creating + migrating if necessary) the SQLite database at
// an already-resolved Paths, for callers (e.g. runInit) that need to
// resolve paths themselves before the DB is ready.
func openDBAt(p paths.Paths) (*sql.DB, error) {
	return store.Open(p.DBPath)
}
