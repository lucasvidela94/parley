package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGz(t *testing.T, name, content string) []byte {
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
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fixture struct {
	asset     []byte
	checksum  string
	withAsset bool
}

func server(t *testing.T, f fixture) (*httptest.Server, *Client) {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		assets := []map[string]string{
			{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
		}
		if f.withAsset {
			assets = append(assets, map[string]string{"name": "parley_linux_amd64.tar.gz", "browser_download_url": srv.URL + "/dl/asset"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0", "assets": assets})
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  parley_linux_amd64.tar.gz\n", f.checksum)
	})
	mux.HandleFunc("/dl/asset", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(f.asset)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{Owner: "o", Repo: "r", HTTP: srv.Client(), APIBase: srv.URL, GOOS: "linux", GOARCH: "amd64"}
	return srv, c
}

func TestLatest(t *testing.T) {
	asset := tarGz(t, "parley", "new binary")
	sum := sha256.Sum256(asset)
	_, c := server(t, fixture{asset: asset, checksum: hex.EncodeToString(sum[:]), withAsset: true})

	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.2.0" || rel.Asset != "parley_linux_amd64.tar.gz" || rel.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("%+v", rel)
	}
}

func TestLatestMissingAsset(t *testing.T) {
	_, c := server(t, fixture{checksum: "deadbeef"})
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("expected error when the platform asset is missing")
	}
}

func TestInstallReplacesBinary(t *testing.T) {
	asset := tarGz(t, "parley", "new binary")
	sum := sha256.Sum256(asset)
	_, c := server(t, fixture{asset: asset, checksum: hex.EncodeToString(sum[:]), withAsset: true})
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "parley")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(context.Background(), rel, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Fatalf("target not replaced: %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("binary is not executable: %v", info.Mode())
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	asset := tarGz(t, "parley", "tampered")
	_, c := server(t, fixture{asset: asset, checksum: "0000000000000000000000000000000000000000000000000000000000000000", withAsset: true})
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "parley")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(context.Background(), rel, target); err == nil {
		t.Fatal("expected checksum error")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("target changed despite checksum failure: %q", got)
	}
}

func TestInstallMissingEntry(t *testing.T) {
	asset := tarGz(t, "other", "nope")
	sum := sha256.Sum256(asset)
	_, c := server(t, fixture{asset: asset, checksum: hex.EncodeToString(sum[:]), withAsset: true})
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Install(context.Background(), rel, filepath.Join(t.TempDir(), "parley")); err == nil {
		t.Fatal("expected error when the archive has no parley binary")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.1.1", true},
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.1.9", false},
		{"v0.1.0", "v0.1.0", false},
		{"0.1.0", "v0.1.0", false},
		{"dev", "v0.1.0", true},
		{"v1.0.0-rc1", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q,%q)=%v want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if AssetName("linux", "arm64") != "parley_linux_arm64.tar.gz" {
		t.Fatal(AssetName("linux", "arm64"))
	}
	if AssetName("windows", "amd64") != "parley_windows_amd64.zip" {
		t.Fatal(AssetName("windows", "amd64"))
	}
}

func TestLatestSendsToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.3.0", "assets": []any{}})
	}))
	defer srv.Close()

	c := &Client{Owner: "o", Repo: "r", HTTP: srv.Client(), APIBase: srv.URL, GOOS: "linux", GOARCH: "amd64", Token: "secret"}
	_, _ = c.Latest(context.Background())
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization header: %q", gotAuth)
	}
}

func TestRateLimitHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{Owner: "o", Repo: "r", HTTP: srv.Client(), APIBase: srv.URL, GOOS: "linux", GOARCH: "amd64"}
	_, err := c.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("expected a token hint, got %v", err)
	}
}
