// Package claudesettings reads, backs up, and patches the Claude Code
// settings.json file that infrapal-token-advisor's env vars must live in for the `claude`
// process to pick them up: exported shell vars silently lose to whatever's
// already in settings.json.
package claudesettings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Doc is a settings.json document, kept as a generic map so that keys
// infrapal-token-advisor doesn't know about (and doesn't need to) round-trip untouched.
// This does not preserve original key order or formatting — Go's
// map[string]any has no stable order, so a merged file's key order can
// differ from the original even though every original key and value not
// touched by the merge survives unchanged. json.MarshalIndent's 2-space
// style is used for the rewrite regardless of the original file's style.
type Doc map[string]any

// Load reads path and parses it as a Doc. If the file doesn't exist, Load
// returns an empty Doc and existed=false rather than an error, since
// settings.json is not guaranteed to exist before `token-advisor init` runs.
func Load(path string) (doc Doc, existed bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Doc{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc == nil {
		doc = Doc{}
	}
	return doc, true, nil
}

// Backup copies path to path+".infrapal-token-advisor-backup-<unix-timestamp>" and returns
// the backup's path. If path doesn't exist yet, there's nothing to back up
// and Backup returns "" with no error.
func Backup(path string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	backupPath := fmt.Sprintf("%s.infrapal-token-advisor-backup-%d", path, now.Unix())
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write backup %s: %w", backupPath, err)
	}
	return backupPath, nil
}

// EnvBlock returns doc's "env" sub-object, creating it if absent. Panics if
// "env" exists but isn't a JSON object — that would mean a hand-edited
// settings.json is already malformed in a way infrapal-token-advisor can't safely merge
// into, so the caller should surface that as an error before calling this.
func (doc Doc) EnvBlock() (map[string]any, error) {
	raw, ok := doc["env"]
	if !ok || raw == nil {
		env := map[string]any{}
		doc["env"] = env
		return env, nil
	}
	env, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`"env" key in settings.json is not an object (got %T) — fix it by hand before running init`, raw)
	}
	return env, nil
}

// MergeEnv sets every key in wanted inside doc's env block, leaving every
// other key (in env or elsewhere in doc) untouched. It returns the subset
// of wanted whose value actually changed (added or different from what was
// already there), in the order supplied by wanted's keys sorted
// alphabetically, so callers can print a stable, deterministic diff.
func MergeEnv(doc Doc, wanted map[string]string) ([]Change, error) {
	env, err := doc.EnvBlock()
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(wanted))
	for k := range wanted {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var changes []Change
	for _, k := range keys {
		newVal := wanted[k]
		oldVal, existed := env[k]
		if existed {
			if oldStr, ok := oldVal.(string); ok && oldStr == newVal {
				continue
			}
		}
		env[k] = newVal
		changes = append(changes, Change{Key: k, OldValue: oldVal, NewValue: newVal, Existed: existed})
	}
	return changes, nil
}

// Change describes one env key infrapal-token-advisor added or overwrote in settings.json.
type Change struct {
	Key      string
	OldValue any // nil if the key didn't exist before
	NewValue string
	Existed  bool
}

// Save writes doc to path with 0600 permissions (settings.json can contain
// auth tokens), creating its parent directory if necessary.
func Save(path string, doc Doc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings.json: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// Restore overwrites path with the contents of backupPath, used by
// `token-advisor uninstall` to put settings.json back the way init found it.
func Restore(path, backupPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup %s: %w", backupPath, err)
	}
	return os.WriteFile(path, data, 0o600)
}
