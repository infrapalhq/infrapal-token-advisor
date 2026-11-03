// Package localhttp protects the servers that listen on 127.0.0.1. Binding to
// the loopback address keeps other machines out, but not web pages open in the
// user's browser, which can send requests there too.
package localhttp

import (
	"net"
	"net/http"
	"time"
)

// Guard refuses requests that didn't come from this machine's own programs.
//
// The Host check stops DNS rebinding, where a page points its own domain at
// 127.0.0.1 to talk to it as same-origin. The cross-origin check stops other
// sites sending writes.
func Guard(next http.Handler) http.Handler {
	protected := http.NewCrossOriginProtection().Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsLoopbackHost(r.Host) {
			http.Error(w, "this server only answers to localhost", http.StatusForbidden)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

// IsLoopbackHost reports whether a Host header names this machine. It compares
// names rather than resolving them, since resolving is what rebinding abuses.
func IsLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}

// Server returns an http.Server with timeouts, so a connection that never
// finishes its request can't hold resources forever.
func Server(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}
