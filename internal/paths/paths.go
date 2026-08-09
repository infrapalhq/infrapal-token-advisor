// Package paths resolves every filesystem location ccotel reads from or
// writes to: the local data directory, the SQLite database, raw request/
// response body storage, and the Claude Code settings file it patches.
package paths

import (
	"os"
	"path/filepath"
)

const (
	// DirMode is the permission mode for ~/.ccotel — private to the owner,
	// since the tree contains prompts, source fragments, and shell commands.
	DirMode = 0o700
	// FileMode is the permission mode for files written inside ~/.ccotel,
	// notably ccotel.db.
	FileMode = 0o600
)

// Paths holds every resolved location ccotel needs.
type Paths struct {
	Home string // user's home directory

	BaseDir    string // ~/.ccotel
	DBPath     string // ~/.ccotel/ccotel.db
	BodiesDir  string // ~/.ccotel/bodies — raw API bodies in file: mode
	ConfigPath string // ~/.ccotel/config.json

	ClaudeSettingsPath string // ~/.claude/settings.json
}

// Resolve computes all ccotel paths relative to the current user's home
// directory. It does not create anything on disk.
func Resolve() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	base := filepath.Join(home, ".ccotel")
	return Paths{
		Home:               home,
		BaseDir:            base,
		DBPath:             filepath.Join(base, "ccotel.db"),
		BodiesDir:          filepath.Join(base, "bodies"),
		ConfigPath:         filepath.Join(base, "config.json"),
		ClaudeSettingsPath: filepath.Join(home, ".claude", "settings.json"),
	}, nil
}

// EnsureBaseDir creates ~/.ccotel (and ~/.ccotel/bodies) with the correct
// permissions if they don't already exist. It re-asserts the mode on an
// existing directory too, in case it was loosened out from under us.
func (p Paths) EnsureBaseDir() error {
	if err := os.MkdirAll(p.BaseDir, DirMode); err != nil {
		return err
	}
	if err := os.Chmod(p.BaseDir, DirMode); err != nil {
		return err
	}
	return nil
}

// EnsureBodiesDir creates ~/.ccotel/bodies for raw-bodies file: mode.
func (p Paths) EnsureBodiesDir() error {
	if err := os.MkdirAll(p.BodiesDir, DirMode); err != nil {
		return err
	}
	return os.Chmod(p.BodiesDir, DirMode)
}
