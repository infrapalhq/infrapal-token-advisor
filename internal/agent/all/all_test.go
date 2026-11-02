package all

import (
	"path/filepath"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/agenttest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/claudecode"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
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

// The sessions.agent column defaults to this ID, so a session written before an
// adapter names its agent still has a valid one.
func TestDefault_MatchesTheDatabaseDefault(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "token-advisor.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO sessions (session_id) VALUES ('s1')`); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT agent FROM sessions WHERE session_id = 's1'`).Scan(&got); err != nil {
		t.Fatalf("read agent: %v", err)
	}
	if want := string(Default(dir).ID()); got != want {
		t.Errorf("sessions.agent defaults to %q, want the default agent %q", got, want)
	}
}

func TestFind(t *testing.T) {
	if a, ok := Find("", "claude-code"); !ok || a.ID() != "claude-code" {
		t.Errorf("Find(claude-code) = %v, %v", a, ok)
	}
	if _, ok := Find("", "no-such-agent"); ok {
		t.Error("Find should report an unknown agent")
	}
}

// Every listed agent meets the contract, with no effort from its author. For
// anything an agent does beyond this, see agenttest.Run in its own tests.
func TestAgents_MeetTheContract(t *testing.T) {
	ids := map[agent.ID]bool{}
	for _, a := range Agents("") {
		ids[a.ID()] = true
	}
	for id := range ids {
		t.Run(string(id), func(t *testing.T) {
			agenttest.Run(t, agenttest.Suite{
				New: func(home string) agent.Agent {
					a, ok := Find(home, id)
					if !ok {
						t.Fatalf("Find(%q) failed", id)
					}
					return a
				},
				NativeIngest: id == claudecode.ID,
			})
		})
	}
}
