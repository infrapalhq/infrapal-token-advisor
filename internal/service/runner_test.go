package service

import (
	"errors"
	"strings"
	"testing"
)

// fakeRunner records commands instead of running them. fail and output are
// keyed by the start of a command, so "launchctl print-disabled" matches any
// user's domain.
type fakeRunner struct {
	calls  []string
	fail   map[string]error
	output map[string]string
}

func (f *fakeRunner) lookup(cmd string) (string, error) {
	f.calls = append(f.calls, cmd)
	var out string
	for prefix, v := range f.output {
		if strings.HasPrefix(cmd, prefix) {
			out = v
		}
	}
	for prefix, err := range f.fail {
		if strings.HasPrefix(cmd, prefix) {
			return out, err
		}
	}
	return out, nil
}

func (f *fakeRunner) Run(name string, args ...string) error {
	_, err := f.lookup(name + " " + strings.Join(args, " "))
	return err
}

func (f *fakeRunner) Output(name string, args ...string) ([]byte, error) {
	out, err := f.lookup(name + " " + strings.Join(args, " "))
	return []byte(out), err
}

var errCommand = errors.New("command failed")

// wantCalls compares the commands that ran, in order.
func wantCalls(t *testing.T, f *fakeRunner, want ...string) {
	t.Helper()
	got := strings.Join(f.calls, "\n")
	if w := strings.Join(want, "\n"); got != w {
		t.Errorf("commands:\n%s\nwant:\n%s", got, w)
	}
}

func TestExecRunner_RunPutsOutputInTheError(t *testing.T) {
	err := execRunner{}.Run("sh", "-c", "echo not loaded; exit 3")
	if err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Errorf("err = %v, want the command's output in it", err)
	}
	if err := (execRunner{}).Run("sh", "-c", "exit 0"); err != nil {
		t.Errorf("a passing command: %v", err)
	}
}

func TestExecRunner_OutputKeepsStdoutOnFailure(t *testing.T) {
	out, err := execRunner{}.Output("sh", "-c", "echo disabled; exit 1")
	if err == nil {
		t.Error("want the exit status as an error")
	}
	if strings.TrimSpace(string(out)) != "disabled" {
		t.Errorf("out = %q, want the stdout kept: is-enabled reports state this way", out)
	}
}
