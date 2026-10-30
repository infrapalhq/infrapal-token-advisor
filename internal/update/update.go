// Package update finds the latest GitHub release and replaces the running
// binary with it. It only ever runs when someone asks it to: token-advisor
// makes no network calls of its own otherwise.
package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	repo       = "infrapalhq/infrapal-token-advisor"
	binaryName = "token-advisor"
	// maxDownload bounds what's read from the network.
	maxDownload = 200 << 20
)

// Client talks to GitHub. Its base URLs exist so tests can use a local server.
type Client struct {
	HTTP        *http.Client
	APIBase     string
	ReleaseBase string
}

// New returns a Client for the real GitHub.
func New() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 5 * time.Minute},
		APIBase:     "https://api.github.com",
		ReleaseBase: "https://github.com/" + repo + "/releases/download",
	}
}

// Latest returns the tag of the newest release, such as "v1.2.3".
func (c *Client) Latest(ctx context.Context) (string, error) {
	body, err := c.get(ctx, c.APIBase+"/repos/"+repo+"/releases/latest")
	if err != nil {
		return "", err
	}
	defer body.Close()
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(body).Decode(&release); err != nil {
		return "", fmt.Errorf("read release: %w", err)
	}
	if release.TagName == "" {
		return "", errors.New("release has no tag")
	}
	return release.TagName, nil
}

func (c *Client) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// Newer reports whether release is a later version than current. Tags may
// carry a leading v. Anything that isn't plain x.y.z, such as a dev build,
// can't be compared and gives an error.
func Newer(release, current string) (bool, error) {
	r, err := parseVersion(release)
	if err != nil {
		return false, err
	}
	c, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	for i := range r {
		if r[i] != c[i] {
			return r[i] > c[i], nil
		}
	}
	return false, nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != len(out) {
		return out, fmt.Errorf("can't compare version %q", v)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, fmt.Errorf("can't compare version %q", v)
		}
		out[i] = n
	}
	return out, nil
}

// ManagedBy names the package manager that owns the binary at path, or "".
func ManagedBy(path string) string {
	if strings.Contains(path, "/Caskroom/") || strings.Contains(path, "/Cellar/") {
		return "homebrew"
	}
	return ""
}

// Install downloads the release tag for goos/goarch, checks it against the
// release's checksums, and replaces the binary at path. The swap is a rename
// on one filesystem, so a failure at any point leaves the old binary in place.
func (c *Client) Install(ctx context.Context, tag, goos, goarch, path string) error {
	version := strings.TrimPrefix(tag, "v")
	archive := fmt.Sprintf("infrapal-token-advisor_%s_%s_%s.tar.gz", version, goos, goarch)

	want, err := c.checksum(ctx, tag, archive)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".token-advisor-update-*")
	if err != nil {
		return fmt.Errorf("can't write next to %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	defer tmp.Close()

	body, err := c.get(ctx, c.ReleaseBase+"/"+tag+"/"+archive)
	if err != nil {
		return err
	}
	defer body.Close()

	// Written to a temp file, as nothing is unpacked until the sum matches.
	download, err := os.CreateTemp("", "token-advisor-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(download.Name())
	defer download.Close()

	hash := sha256.New()
	limited := &io.LimitedReader{R: body, N: maxDownload + 1}
	if _, err := io.Copy(io.MultiWriter(download, hash), limited); err != nil {
		return fmt.Errorf("download %s: %w", archive, err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("%s is larger than expected", archive)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("%s failed its checksum: got %s, want %s", archive, got, want)
	}

	if _, err := download.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := extractBinary(download, tmp); err != nil {
		return fmt.Errorf("unpack %s: %w", archive, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (c *Client) checksum(ctx context.Context, tag, archive string) (string, error) {
	body, err := c.get(ctx, c.ReleaseBase+"/"+tag+"/checksums.txt")
	if err != nil {
		return "", err
	}
	defer body.Close()
	scanner := bufio.NewScanner(io.LimitReader(body, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == archive {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum for %s in this release", archive)
}

// extractBinary copies the one file it wants out of the archive. Nothing else
// is unpacked, so entry names can't write anywhere unexpected.
func extractBinary(r io.Reader, w io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("no %s in the archive", binaryName)
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg && h.Name == binaryName {
			_, err := io.Copy(w, io.LimitReader(tr, maxDownload))
			return err
		}
	}
}
