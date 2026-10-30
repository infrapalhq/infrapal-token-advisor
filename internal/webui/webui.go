// Package webui embeds the built React dashboard (web/dist) into the infrapal-token-advisor
// binary. dist/ here starts as a checked-in placeholder — go:embed requires
// at least one file to exist at compile time — and scripts/build-web.sh
// overwrites it with the real Vite build output before a release build.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFiles embed.FS

// DistFS returns the embedded frontend build, rooted at dist/ so callers
// see index.html and assets/ directly rather than under a dist/ prefix.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFiles, "dist")
}
