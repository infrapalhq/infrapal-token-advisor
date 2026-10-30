package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveWith(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// release serves one fake release, with a checksum that may not match.
func release(t *testing.T, archive []byte, sum string) *Client {
	t.Helper()
	if sum == "" {
		s := sha256.Sum256(archive)
		sum = hex.EncodeToString(s[:])
	}
	const name = "infrapal-token-advisor_1.2.3_linux_amd64.tar.gz"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/repos/"+repo+"/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v1.2.3"}`)
	})
	mux.HandleFunc("/dl/v1.2.3/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "abc  other.tar.gz\n%s  %s\n", sum, name)
	})
	mux.HandleFunc("/dl/v1.2.3/"+name, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), APIBase: srv.URL + "/api", ReleaseBase: srv.URL + "/dl"}
}

func oldBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLatest(t *testing.T) {
	c := release(t, nil, "x")
	tag, err := c.Latest(context.Background())
	if err != nil || tag != "v1.2.3" {
		t.Errorf("Latest = %q, %v", tag, err)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		release, current string
		want             bool
	}{
		{"v1.2.3", "1.2.2", true},
		{"v1.10.0", "1.9.9", true},
		{"v2.0.0", "1.99.99", true},
		{"v1.2.3", "1.2.3", false},
		{"v1.2.3", "1.3.0", false},
	}
	for _, c := range cases {
		got, err := Newer(c.release, c.current)
		if err != nil || got != c.want {
			t.Errorf("Newer(%s, %s) = %v, %v, want %v", c.release, c.current, got, err, c.want)
		}
	}
	if _, err := Newer("v1.2.3", "dev"); err == nil {
		t.Error("compared against a dev build")
	}
}

func TestManagedBy(t *testing.T) {
	for path, want := range map[string]string{
		"/opt/homebrew/Caskroom/infrapal-token-advisor/1.0/token-advisor": "homebrew",
		"/home/linuxbrew/.linuxbrew/Cellar/x/1.0/bin/token-advisor":       "homebrew",
		"/home/me/.local/bin/token-advisor":                               "",
	} {
		if got := ManagedBy(path); got != want {
			t.Errorf("ManagedBy(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestInstallReplacesBinary(t *testing.T) {
	c := release(t, archiveWith(t, binaryName, "new"), "")
	path := oldBinary(t)

	if err := c.Install(context.Background(), "v1.2.3", "linux", "amd64", path); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Errorf("binary = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o, want 755", info.Mode().Perm())
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".token-advisor-update-*"))
	if len(leftovers) != 0 {
		t.Errorf("left temp files behind: %v", leftovers)
	}
}

func TestInstallKeepsOldBinaryOnFailure(t *testing.T) {
	cases := []struct {
		name    string
		archive []byte
		sum     string
		wantErr string
	}{
		{"bad checksum", archiveWith(t, binaryName, "evil"), strings.Repeat("0", 64), "checksum"},
		{"no binary in archive", archiveWith(t, "README.md", "hi"), "", "no token-advisor"},
		{"traversal name is ignored", archiveWith(t, "../"+binaryName, "evil"), "", "no token-advisor"},
		{"not an archive", []byte("garbage"), "", "unpack"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := release(t, tc.archive, tc.sum)
			path := oldBinary(t)

			err := c.Install(context.Background(), "v1.2.3", "linux", "amd64", path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Install = %v, want an error about %q", err, tc.wantErr)
			}
			if got, _ := os.ReadFile(path); string(got) != "old" {
				t.Errorf("binary changed to %q", got)
			}
		})
	}
}

func TestInstallMissingPlatform(t *testing.T) {
	c := release(t, archiveWith(t, binaryName, "new"), "")
	path := oldBinary(t)

	err := c.Install(context.Background(), "v1.2.3", "linux", "riscv64", path)
	if err == nil || !strings.Contains(err.Error(), "no checksum") {
		t.Fatalf("Install = %v", err)
	}
}
