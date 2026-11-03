package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

// withPorts writes a config using spare ports, so a test never collides with
// a real collector on the machine.
func withPorts(t *testing.T, home string) (otlp, api int) {
	t.Helper()
	otlp, api = freePort(t), freePort(t)
	dir := filepath.Join(home, ".infrapal-token-advisor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"otlp_port":%d,"api_port":%d,"retention_months":0}`, otlp, api)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return otlp, api
}

// withStop replaces Ctrl+C with a function the test calls.
func withStop(t *testing.T) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	old := untilInterrupted
	untilInterrupted = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	t.Cleanup(func() { untilInterrupted = old; cancel() })
	return cancel
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func otlpPrompt(t *testing.T) []byte {
	t.Helper()
	str := func(s string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
	}
	rl, err := proto.Marshal(&logspb.ResourceLogs{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "session.id", Value: str("sess-start")}}},
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			TimeUnixNano: uint64(time.Now().UnixNano()),
			Attributes: []*commonpb.KeyValue{
				{Key: "event.name", Value: str("claude_code.user_prompt")},
				{Key: "prompt.id", Value: str("p-start")},
			},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), rl)
}

// The collector and dashboard together, as the background service runs them:
// telemetry goes in, the dashboard answers, and Ctrl+C stops both cleanly.
func TestRunStart_CollectsAndServesUntilStopped(t *testing.T) {
	home := withFakeHome(t)
	otlpPort, apiPort := withPorts(t, home)
	stop := withStop(t)

	done := make(chan error, 1)
	go func() { done <- runStart(nil) }()

	collector := fmt.Sprintf("http://127.0.0.1:%d/v1/logs", otlpPort)
	waitFor(t, "the collector", func() bool {
		resp, err := http.Post(collector, "application/x-protobuf", bytes.NewReader(otlpPrompt(t)))
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	waitFor(t, "the dashboard", func() bool { return dashboardRunning(apiPort) })

	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the prompt to be stored", func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status", apiPort))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var st map[string]any
		return json.NewDecoder(resp.Body).Decode(&st) == nil && st["last_event_ms"] != nil
	})

	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runStart: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runStart didn't stop")
	}
	if dashboardRunning(apiPort) {
		t.Error("the dashboard should stop with the collector")
	}
	if _, err := os.Stat(p.LogPath); err != nil {
		t.Errorf("the collector should write its log: %v", err)
	}
}

func TestRunStart_FailsWhenThePortIsTaken(t *testing.T) {
	home := withFakeHome(t)
	otlpPort, _ := withPorts(t, home)
	withStop(t)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", otlpPort))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() { done <- runStart(nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("want an error when the collector can't listen")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runStart didn't give up")
	}
}

func TestRunUI_ServesWhenNoServiceIsRunning(t *testing.T) {
	home := withFakeHome(t)
	_, apiPort := withPorts(t, home)
	stop := withStop(t)
	opened := ""
	old := openBrowser
	openBrowser = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openBrowser = old })

	done := make(chan error, 1)
	go func() { done <- runUI(nil) }()
	waitFor(t, "the dashboard", func() bool { return dashboardRunning(apiPort) })
	stop()
	if err := <-done; err != nil {
		t.Fatalf("runUI: %v", err)
	}
	if want := fmt.Sprintf("http://127.0.0.1:%d", apiPort); opened != want {
		t.Errorf("opened %q, want %q", opened, want)
	}
}

func TestRunUI_UsesTheRunningService(t *testing.T) {
	home := withFakeHome(t)
	_, apiPort := withPorts(t, home)
	running := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", apiPort))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = running.Serve(ln) }()
	defer running.Close()

	opened := ""
	old := openBrowser
	openBrowser = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openBrowser = old })

	out := stdout(t, func() error { return runUI(nil) })
	if opened == "" {
		t.Error("should open the running dashboard")
	}
	if want := fmt.Sprintf("http://127.0.0.1:%d", apiPort); !bytes.Contains([]byte(out), []byte(want)) {
		t.Errorf("output should name %s:\n%s", want, out)
	}
}
