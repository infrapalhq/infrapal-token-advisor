package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/all"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

// secondAgent is another supported agent, recording what the CLI asks of it.
type secondAgent struct {
	agent.Agent
	connects, disconnects int
	lastDisconnect        agent.Connection
}

func (a *secondAgent) ID() agent.ID { return "second" }
func (a *secondAgent) Name() string { return "Second Agent" }

func (a *secondAgent) Connect(io.Writer, agent.ConnectOptions) (agent.Connection, error) {
	a.connects++
	return agent.Connection{BackupPath: "/second/backup"}, nil
}

func (a *secondAgent) Disconnect(_ io.Writer, conn agent.Connection) error {
	a.disconnects++
	a.lastDisconnect = conn
	return nil
}

func (a *secondAgent) Check(int) ([]agent.Check, error) {
	return []agent.Check{{Label: "second agent looks fine", Pass: true}}, nil
}

func withSecondAgent(t *testing.T) *secondAgent {
	t.Helper()
	second := &secondAgent{}
	old := agentList
	agentList = func(home string) []agent.Agent { return append(all.Agents(home), second) }
	t.Cleanup(func() { agentList = old })
	return second
}

func stdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = old
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestInit_AgentFlagConnectsThatAgent(t *testing.T) {
	home := withFakeHome(t)
	withFakeService(t)
	second := withSecondAgent(t)

	if err := runInit([]string{"--agent", "second"}); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if second.connects != 1 {
		t.Errorf("connects = %d", second.connects)
	}
	cfg := readConfig(t, home)
	if got := cfg.Agents["second"].BackupPath; got != "/second/backup" {
		t.Errorf("backup = %q", got)
	}
	if _, ok := cfg.Agents["claude-code"]; ok {
		t.Error("Claude Code wasn't asked for")
	}
	if _, err := os.Stat(home + "/.claude/settings.json"); !os.IsNotExist(err) {
		t.Errorf("another agent's init mustn't touch Claude's settings: %v", err)
	}
}

func TestInit_NoFlagConnectsTheDefault(t *testing.T) {
	home := withFakeHome(t)
	withFakeService(t)
	second := withSecondAgent(t)

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if second.connects != 0 {
		t.Error("the second agent wasn't asked for")
	}
	if _, ok := readConfig(t, home).Agents["claude-code"]; !ok {
		t.Error("Claude Code should be recorded as connected")
	}
}

func TestInit_UnknownAgentIsRefusedBeforeAnythingHappens(t *testing.T) {
	home := withFakeHome(t)
	fake := withFakeService(t)

	err := runInit([]string{"--agent", "nope"})
	if err == nil || !strings.Contains(err.Error(), "claude-code") {
		t.Fatalf("err = %v, want it to list the choices", err)
	}
	if fake.installCalls != 0 {
		t.Error("the service shouldn't be installed")
	}
	if _, err := os.Stat(home + "/.infrapal-token-advisor"); !os.IsNotExist(err) {
		t.Errorf("nothing should be created: %v", err)
	}
}

func TestUninstall_DisconnectsEveryAgent(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)
	second := withSecondAgent(t)
	for _, args := range [][]string{nil, {"--agent", "second"}} {
		if err := runInit(args); err != nil {
			t.Fatalf("runInit: %v", err)
		}
	}
	if err := runUninstall(nil); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	if second.disconnects != 1 || second.lastDisconnect.BackupPath != "/second/backup" {
		t.Errorf("disconnects = %d, conn = %+v", second.disconnects, second.lastDisconnect)
	}
}

func TestUninstall_AgentFlagKeepsEverythingElse(t *testing.T) {
	home := withFakeHome(t)
	fake := withFakeService(t)
	second := withSecondAgent(t)
	for _, args := range [][]string{nil, {"--agent", "second"}} {
		if err := runInit(args); err != nil {
			t.Fatalf("runInit: %v", err)
		}
	}
	if err := runUninstall([]string{"--agent", "second"}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	if second.disconnects != 1 {
		t.Errorf("disconnects = %d", second.disconnects)
	}
	if fake.uninstallCalls != 0 {
		t.Error("the service must stay for the other agent")
	}
	cfg := readConfig(t, home)
	if _, ok := cfg.Agents["second"]; ok {
		t.Error("the agent should be forgotten")
	}
	if _, ok := cfg.Agents["claude-code"]; !ok {
		t.Error("the other agent should stay connected")
	}
	if _, err := os.Stat(home + "/.infrapal-token-advisor/token-advisor.db"); err != nil {
		t.Errorf("data should stay: %v", err)
	}
}

func TestDoctor_ChecksEachConnectedAgent(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)
	withSecondAgent(t)
	for _, args := range [][]string{nil, {"--agent", "second"}} {
		if err := runInit(args); err != nil {
			t.Fatalf("runInit: %v", err)
		}
	}
	out := stdout(t, func() error { return runDoctor(nil) })
	for _, want := range []string{"[INFO] Claude Code:", "[INFO] Second Agent:", "second agent looks fine"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestConnectedAgents_DefaultsWhenNoneAreRecorded(t *testing.T) {
	withSecondAgent(t)
	got := connectedAgents("", config.Config{})
	if len(got) != 1 || got[0].ID() != "claude-code" {
		t.Errorf("got %v", got)
	}
}

func TestStatus_ListsConnectedAgents(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)
	if err := runInit(nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	db, err := openDBAt(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (session_id) VALUES ('s1'), ('s2')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if out := stdout(t, func() error { return runStatus(nil) }); !strings.Contains(out, "agents:      Claude Code (2 sessions)") {
		t.Errorf("status output:\n%s", out)
	}
}
