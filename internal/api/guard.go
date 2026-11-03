package api

import (
	"net/http"

	"github.com/infrapalhq/infrapal-token-advisor/internal/localhttp"
)

// guard protects the API from web pages open in the user's browser, which
// matters because it holds prompts and runs for as long as the collector
// does. See localhttp.Guard.
func guard(next http.Handler) http.Handler {
	return localhttp.Guard(next)
}
