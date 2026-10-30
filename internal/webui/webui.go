// Package webui embeds the built React dashboard (web/dist) into the infrapal-token-advisor
// binary. scripts/build-web.sh fills dist/, which git ignores apart from a
// .gitkeep, since go:embed needs at least one file at compile time. A binary
// built without running it serves the placeholder page instead.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFiles embed.FS

//go:embed placeholder
var placeholderFiles embed.FS

// DistFS returns the embedded frontend, rooted so callers see index.html and
// assets/ directly rather than under a dist/ prefix.
func DistFS() (fs.FS, error) {
	dist, err := fs.Sub(distFiles, "dist")
	if err != nil {
		return nil, err
	}
	placeholder, err := fs.Sub(placeholderFiles, "placeholder")
	if err != nil {
		return nil, err
	}
	return chooseFS(dist, placeholder), nil
}

// chooseFS returns dist if it holds a dashboard build, else placeholder.
func chooseFS(dist, placeholder fs.FS) fs.FS {
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return placeholder
	}
	return dist
}
