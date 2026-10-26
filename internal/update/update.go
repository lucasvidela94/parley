// Package update replaces the running binary with the latest GitHub release.
package update

import (
	"archive/tar"
	"archive/zip"
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
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPI = "https://api.github.com"
	// maxBinarySize bounds what an archive may unpack, so a malicious release
	// cannot fill the disk.
	maxBinarySize = 256 << 20
)

type Release struct {
	Tag    string
	Asset  string
	URL    string
	SHA256 string
}

type Client struct {
	Owner, Repo string
	HTTP        *http.Client
	APIBase     string
	GOOS        string
	GOARCH      string
}

func New(owner, repo string) *Client {
	return &Client{
		Owner: owner, Repo: repo,
		HTTP:    &http.Client{Timeout: 90 * time.Second},
		APIBase: defaultAPI,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
	}
}

// Latest finds the newest release and the asset for this platform, with its
// expected checksum.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.APIBase, c.Owner, c.Repo)
	if err := c.getJSON(ctx, url, &payload); err != nil {
		return Release{}, err
	}
	if payload.TagName == "" {
		return Release{}, errors.New("release has no tag")
	}
	want := AssetName(c.GOOS, c.GOARCH)
	var assetURL, checksumsURL string
	for _, a := range payload.Assets {
		switch a.Name {
		case want:
			assetURL = a.URL
		case "checksums.txt":
			checksumsURL = a.URL
		}
	}
	if assetURL == "" {
		return Release{}, fmt.Errorf("release %s has no asset %s", payload.TagName, want)
	}
	if checksumsURL == "" {
		return Release{}, fmt.Errorf("release %s has no checksums.txt", payload.TagName)
	}
	sum, err := c.checksum(ctx, checksumsURL, want)
	if err != nil {
		return Release{}, err
	}
	return Release{Tag: payload.TagName, Asset: want, URL: assetURL, SHA256: sum}, nil
}

// Install downloads the release asset, verifies its checksum and atomically
// replaces target with the binary inside it.
func (c *Client) Install(ctx context.Context, rel Release, target string) error {
	dir := filepath.Dir(target)
	asset, err := os.CreateTemp(dir, ".parley-download-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(asset.Name()) }()
	defer func() { _ = asset.Close() }()

	sum, err := c.download(ctx, rel.URL, asset)
	if err != nil {
		return err
	}
	if !strings.EqualFold(sum, rel.SHA256) {
		return fmt.Errorf("checksum mismatch for %s: got %s want %s", rel.Asset, sum, rel.SHA256)
	}
	if err := asset.Close(); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".parley-new-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	defer func() { _ = tmp.Close() }()

	if err := extract(asset.Name(), BinaryName(), tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil { //nolint:gosec // an updated binary must be executable
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("could not replace %s (on Windows, close the running binary first): %w", target, err)
	}
	return nil
}

// IsNewer reports whether latest should replace current. Unparseable versions
// fall back to plain inequality.
func IsNewer(current, latest string) bool {
	c, ok1 := parseVersion(current)
	l, ok2 := parseVersion(latest)
	if !ok1 || !ok2 {
		return current != latest
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func AssetName(goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("parley_%s_%s.zip", goos, goarch)
	}
	return fmt.Sprintf("parley_%s_%s.tar.gz", goos, goarch)
}

func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "parley.exe"
	}
	return "parley"
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "parley")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) checksum(ctx context.Context, url, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "parley")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums: %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset {
			return fields[0], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum for %s", asset)
}

func (c *Client) download(ctx context.Context, url string, dst io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "parley")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: %s", resp.Status)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extract(assetPath, want string, out io.Writer) error {
	switch {
	case strings.HasSuffix(assetPath, ".zip"):
		return extractZip(assetPath, want, out)
	default:
		return extractTarGz(assetPath, want, out)
	}
}

func extractTarGz(path, want string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s not found in %s", want, filepath.Base(path))
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != want {
			continue
		}
		if h.Size > maxBinarySize {
			return fmt.Errorf("%s is larger than %d bytes", want, maxBinarySize)
		}
		_, err = io.CopyN(out, tr, h.Size)
		return err
	}
}

func extractZip(path, want string, out io.Writer) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		if f.UncompressedSize64 > maxBinarySize {
			return fmt.Errorf("%s is larger than %d bytes", want, maxBinarySize)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.CopyN(out, rc, int64(f.UncompressedSize64))
		_ = rc.Close()
		return err
	}
	return fmt.Errorf("%s not found in %s", want, filepath.Base(path))
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, len(parts) > 0
}
