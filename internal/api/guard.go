package api

import (
	"net"
	"net/http"
)

// guard protects the API from web pages open in the user's browser. Binding
// to 127.0.0.1 keeps other machines out, but not those pages, and this server
// runs for as long as the collector does.
//
// The Host check stops DNS rebinding, where a page points its own domain at
// 127.0.0.1 to read responses as same-origin. The cross-origin check stops
// other sites sending writes to this API.
func guard(next http.Handler) http.Handler {
	protected := http.NewCrossOriginProtection().Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			writeError(w, http.StatusForbidden, "this API only answers to localhost")
			return
		}
		protected.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether a Host header names this machine. It compares
// names rather than resolving them, since resolving is what rebinding abuses.
func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}
