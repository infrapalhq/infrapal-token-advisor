// Package paths resolves every filesystem location infrapal-token-advisor
// reads from or writes to: the local data directory, the SQLite database, raw
// request/response body storage, and the Claude Code settings file it patches.
package paths

import (
	"os"
	"path/filepath"
)

const (
	// DirMode is the permission mode for ~/.infrapal-token-advisor — private to the owner,
	// since the tree contains prompts, source fragments, and shell commands.
	DirMode = 0o700
	// FileMode is the permission mode for files written inside ~/.infrapal-token-advisor,
	// notably token-advisor.db.
	FileMode = 0o600
)

// Paths holds every resolved location infrapal-token-advisor needs.
type Paths struct {
	Home string // user's home directory

	BaseDir    string // ~/.infrapal-token-advisor
	DBPath     string // ~/.infrapal-token-advisor/token-advisor.db
	BodiesDir  string // ~/.infrapal-token-advisor/bodies — raw API bodies in file: mode
	ConfigPath string // ~/.infrapal-token-advisor/config.json

	ClaudeSettingsPath string // ~/.claude/settings.json
}

// Resolve computes all infrapal-token-advisor paths relative to the current user's home
// directory. It does not create anything on disk.
func Resolve() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	base := filepath.Join(home, ".infrapal-token-advisor")
	return Paths{
		Home:               home,
		BaseDir:            base,
		DBPath:             filepath.Join(base, "token-advisor.db"),
		BodiesDir:          filepath.Join(base, "bodies"),
		ConfigPath:         filepath.Join(base, "config.json"),
		ClaudeSettingsPath: filepath.Join(home, ".claude", "settings.json"),
	}, nil
}

// EnsureBaseDir creates ~/.infrapal-token-advisor (and ~/.infrapal-token-advisor/bodies) with the correct
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

// EnsureBodiesDir creates ~/.infrapal-token-advisor/bodies for raw-bodies file: mode.
func (p Paths) EnsureBodiesDir() error {
	if err := os.MkdirAll(p.BodiesDir, DirMode); err != nil {
		return err
	}
	return os.Chmod(p.BodiesDir, DirMode)
}
