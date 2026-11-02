package all

import (
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

func TestAgents_HaveUniqueIDsAndNames(t *testing.T) {
	seen := map[agent.ID]bool{}
	for _, a := range Agents(t.TempDir()) {
		if a.ID() == "" || a.Name() == "" {
			t.Errorf("%T has no ID or name", a)
		}
		if seen[a.ID()] {
			t.Errorf("two agents use the ID %q", a.ID())
		}
		seen[a.ID()] = true
	}
}

func TestDefault_IsClaudeCode(t *testing.T) {
	if got := Default(t.TempDir()).ID(); got != "claude-code" {
		t.Errorf("Default = %q, want claude-code, so existing installs keep working", got)
	}
}
