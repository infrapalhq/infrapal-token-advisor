package mcpserver

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/infrapalhq/infrapal-token-advisor/internal/api"
)

// The overview is an MCP App: get_overview links to this ui:// resource, and
// hosts that support MCP Apps, such as Claude, draw it beside the answer. Hosts
// that don't support them ignore the link and use the tool's text instead.
//
// The view is one self-contained HTML file with no external requests, so it
// works under the hosts' default content security policy, which blocks them.
const (
	overviewURI = "ui://infrapal-token-advisor/overview.html"
	appMimeType = "text/html;profile=mcp-app"
)

//go:embed ui/overview.html
var overviewHTML string

var resources = []map[string]any{{
	"uri":         overviewURI,
	"name":        "overview",
	"title":       "Token Advisor overview",
	"description": "Spend, tokens, cache reuse and the top findings, drawn by get_overview.",
	"mimeType":    appMimeType,
}}

// readResource returns a resource's contents.
func readResource(params json.RawMessage) (any, *rpcError) {
	var in struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, &rpcError{codeInvalidParams, "invalid params"}
	}
	if in.URI != overviewURI {
		return nil, &rpcError{codeResourceNotFound, "resource not found: " + in.URI}
	}
	return map[string]any{"contents": []map[string]any{{
		"uri":      overviewURI,
		"mimeType": appMimeType,
		"text":     overviewHTML,
		"_meta":    map[string]any{"ui": map[string]any{"prefersBorder": true}},
	}}}, nil
}

// overview is get_overview's result: the safe overview, plus where the
// dashboard is, so the view can offer to open it.
type overview struct {
	api.SafeOverview
	DashboardURL string `json:"dashboard_url,omitempty"`
}

func (s *Server) getOverview(ctx context.Context, args json.RawMessage) (any, error) {
	var in struct {
		Since string `json:"since"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Since == "" {
		in.Since = "30d"
	}
	o, err := api.SafeOverviewFor(ctx, s.db, in.Since)
	if err != nil {
		return nil, err
	}
	return overview{SafeOverview: o, DashboardURL: s.dashboardURL}, nil
}
