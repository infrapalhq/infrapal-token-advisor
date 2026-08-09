package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// newSPAHandler serves the embedded dashboard build out of uiFS. Requests
// for a path that exists as a real file (e.g. /assets/index-abc123.js) are
// served as-is; anything else (e.g. /waterfall, a client-side route) falls
// back to index.html so the React Router app can take over.
func newSPAHandler(uiFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(uiFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(uiFS, p); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
