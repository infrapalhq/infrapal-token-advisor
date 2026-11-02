// Package all lists every supported agent. It's separate from package agent
// so that adapters can import that package.
package all

import (
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/claudecode"
)

// Agents returns every supported agent for the user with this home directory.
// The first is the default. The home can be empty for a caller that only asks
// what an agent is, rather than setting one up.
func Agents(home string) []agent.Agent {
	return []agent.Agent{
		claudecode.New(home),
	}
}

// Find returns the agent with this ID, if this build supports it.
func Find(home string, id agent.ID) (agent.Agent, bool) {
	for _, a := range Agents(home) {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}

// Default returns the agent used when none is named.
func Default(home string) agent.Agent {
	return Agents(home)[0]
}
