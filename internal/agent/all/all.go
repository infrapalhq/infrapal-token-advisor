// Package all lists every supported agent. It's separate from package agent
// so that adapters can import that package.
package all

import (
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/claudecode"
)

// Agents returns every supported agent for the user with this home directory.
// The first is the default.
func Agents(home string) []agent.Agent {
	return []agent.Agent{
		claudecode.New(home),
	}
}

// Default returns the agent used when none is named.
func Default(home string) agent.Agent {
	return Agents(home)[0]
}
