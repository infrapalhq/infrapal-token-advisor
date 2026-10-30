package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// backupsKept is how many pre-migration backups stay on disk.
	backupsKept = 2
	backupInfix = ".backup-"
)

// backupDatabase writes a consistent copy of the database next to it, named
// for the schema version it holds. VACUUM INTO reads through the WAL, so
// unlike a file copy it's safe while other processes have the database open.
func backupDatabase(db *sql.DB, dbPath string, version int, now time.Time) (string, error) {
	// CreateTemp makes it unique, as another process may be doing this too,
	// and private before anything is in it, as it holds prompts. VACUUM INTO
	// accepts an existing file only if it's empty.
	pattern := fmt.Sprintf("%s%sv%d-%d-*", filepath.Base(dbPath), backupInfix, version, now.UnixMilli())
	f, err := os.CreateTemp(filepath.Dir(dbPath), pattern)
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()

	if _, err := db.Exec(`VACUUM INTO ?`, path); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// Backups returns the backups of dbPath, oldest first.
func Backups(dbPath string) ([]string, error) {
	paths, err := filepath.Glob(escapeGlob(dbPath) + backupInfix + "*")
	if err != nil {
		return nil, err
	}
	sort.Slice(paths, func(i, j int) bool { return modTime(paths[i]).Before(modTime(paths[j])) })
	return paths, nil
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func pruneBackups(dbPath string, keep int) error {
	paths, err := Backups(dbPath)
	if err != nil || len(paths) <= keep {
		return err
	}
	var errs []error
	for _, path := range paths[:len(paths)-keep] {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RemoveBackups deletes backups last written before cutoff, returning how many
// went. They hold everything the database held, so a backup must not outlive
// a retention limit or a purge for long.
func RemoveBackups(dbPath string, cutoff time.Time) (int, error) {
	paths, err := Backups(dbPath)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, path := range paths {
		if !modTime(path).Before(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// escapeGlob stops characters in a path, such as a [ in a home directory,
// being read as glob syntax.
func escapeGlob(path string) string {
	out := make([]rune, 0, len(path))
	for _, r := range path {
		if r == '*' || r == '?' || r == '[' || r == '\\' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}
