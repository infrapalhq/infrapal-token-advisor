package cli

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/all"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
)

// agentList is all.Agents behind a seam, so tests can add a second agent.
var agentList = all.Agents

// supportedAgents names the agents a flag accepts, default first.
func supportedAgents() string {
	var ids []string
	for _, a := range agentList("") {
		ids = append(ids, string(a.ID()))
	}
	return strings.Join(ids, ", ") + " (the first is the default)"
}

// chooseAgent finds the agent a flag names. Empty means the default.
func chooseAgent(home, name string) (agent.Agent, error) {
	agents := agentList(home)
	if name == "" {
		return agents[0], nil
	}
	for _, a := range agents {
		if string(a.ID()) == name {
			return a, nil
		}
	}
	return nil, fmt.Errorf("unknown agent %q: choose from %s", name, supportedAgents())
}

// connectedAgents returns the agents init has connected. An install with none
// recorded is treated as the default agent's, so doctor and uninstall still
// have something to work on. That covers init having created settings from
// nothing on an older build, and a machine where init never ran.
func connectedAgents(home string, cfg config.Config) []agent.Agent {
	agents := agentList(home)
	var connected []agent.Agent
	for _, a := range agents {
		if _, ok := cfg.Agents[string(a.ID())]; ok {
			connected = append(connected, a)
		}
	}
	if len(connected) == 0 {
		return agents[:1]
	}
	return connected
}

// agentsStatus is the status line listing each connected agent's sessions.
func agentsStatus(db *sql.DB, home string, cfg config.Config) (string, error) {
	var parts []string
	for _, a := range connectedAgents(home, cfg) {
		var n int64
		err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE agent = ?`, string(a.ID())).Scan(&n)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("%s (%d sessions)", a.Name(), n))
	}
	return strings.Join(parts, ", "), nil
}
