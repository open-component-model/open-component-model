package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd/version"
)

// ---- unit tests: pure functions ----

func TestSemverFromTag(t *testing.T) {
	tests := []struct {
		tag     string
		want    string
		wantErr bool
	}{
		{"cli/v0.2.0", "0.2.0", false},
		{"cli/v0.2.0-rc.1", "0.2.0-rc.1", false},
		{"cli/v1.2.3", "1.2.3", false},
		{"v0.2.0", "", true},
		{"0.2.0", "", true},
		{"some/v0.2.0", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			got, err := semverFromTag(tt.tag)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeVersionTag(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v0.2.0", "cli/v0.2.0"},
		{"0.2.0", "cli/v0.2.0"},
		{"cli/v0.2.0", "cli/v0.2.0"},
		{"v1.0.0-rc.1", "cli/v1.0.0-rc.1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeVersionTag(tt.input))
		})
	}
}

func TestIsUpdateAvailable(t *testing.T) {
	tests := []struct {
		current   string
		candidate string
		want      bool
		wantErr   bool
	}{
		{"v0.1.0", "v0.2.0", true, false},
		{"v0.2.0", "v0.1.0", false, false},
		{"v0.2.0", "v0.2.0", false, false},
		{"v0.2.0", "v0.2.1-rc.1", true, false},
		{"v0.2.1", "v0.2.1-rc.1", false, false},
		{"n/a", "v0.2.0", false, true},
		{"v0.2.0", "not-semver", false, true},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s_vs_%s", tt.current, tt.candidate)
		t.Run(name, func(t *testing.T) {
			got, err := isUpdateAvailable(tt.current, tt.candidate)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindAsset(t *testing.T) {
	release := &githubRelease{
		TagName: "cli/v0.2.0",
		Assets: []githubAsset{
			{Name: "ocm-linux-amd64", BrowserDownloadURL: "https://example.com/ocm-linux-amd64", Size: 100},
			{Name: "ocm-linux-arm64", BrowserDownloadURL: "https://example.com/ocm-linux-arm64", Size: 100},
			{Name: "ocm-darwin-amd64", BrowserDownloadURL: "https://example.com/ocm-darwin-amd64", Size: 100},
			{Name: "ocm-darwin-arm64", BrowserDownloadURL: "https://example.com/ocm-darwin-arm64", Size: 100},
			{Name: "ocm-windows-amd64", BrowserDownloadURL: "https://example.com/ocm-windows-amd64", Size: 100},
			{Name: "ocm-windows-arm64", BrowserDownloadURL: "https://example.com/ocm-windows-arm64", Size: 100},
		},
	}

	asset, err := findAsset(release)
	require.NoError(t, err)
	expected := fmt.Sprintf("ocm-%s-%s", runtime.GOOS, runtime.GOARCH)
	assert.Equal(t, expected, asset.Name)
}

func TestFindAsset_NotFound(t *testing.T) {
	release := &githubRelease{
		TagName: "cli/v0.2.0",
		Assets: []githubAsset{
			{Name: "ocm-plan9-amd64"},
		},
	}
	_, err := findAsset(release)
	require.Error(t, err)
	assert.Contains(t, err.Error(), runtime.GOOS)
	assert.Contains(t, err.Error(), runtime.GOARCH)
}

// ---- integration tests: httptest.Server ----

// newTestServer creates an httptest.Server that serves a fake GitHub Releases API.
func newTestServer(t *testing.T, latestTag string, releases map[string]*githubRelease) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	owner := "open-component-model/open-component-model"

	// /releases/latest
	mux.HandleFunc(fmt.Sprintf("/repos/%s/releases/latest", owner), func(w http.ResponseWriter, r *http.Request) {
		rel, ok := releases[latestTag]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	})

	// /releases/tags/{tag} — must be registered before /releases to avoid ambiguity.
	mux.HandleFunc(fmt.Sprintf("/repos/%s/releases/tags/", owner), func(w http.ResponseWriter, r *http.Request) {
		prefix := fmt.Sprintf("/repos/%s/releases/tags/", owner)
		tag := strings.TrimPrefix(r.URL.Path, prefix)
		rel, ok := releases[tag]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	})

	// /releases (list) — for --pre-release path.
	mux.HandleFunc(fmt.Sprintf("/repos/%s/releases", owner), func(w http.ResponseWriter, r *http.Request) {
		list := make([]githubRelease, 0, len(releases))
		for _, rel := range releases {
			list = append(list, *rel)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// withTestServer redirects all GitHub API calls to the given httptest.Server.
func withTestServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	origDo := httpDoFunc
	httpDoFunc = func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.URL.Scheme = "http"
		req.URL.Host = srv.Listener.Addr().String()
		return http.DefaultClient.Do(req)
	}
	t.Cleanup(func() { httpDoFunc = origDo })
}

func makeRelease(tag string, prerelease bool, assetNames ...string) *githubRelease {
	assets := make([]githubAsset, 0, len(assetNames))
	for _, name := range assetNames {
		assets = append(assets, githubAsset{
			Name:               name,
			BrowserDownloadURL: "http://example.com/" + name,
			Size:               1024,
		})
	}
	return &githubRelease{
		TagName:    tag,
		Prerelease: prerelease,
		Assets:     assets,
	}
}

func currentPlatformAsset() string {
	return fmt.Sprintf("ocm-%s-%s", runtime.GOOS, runtime.GOARCH)
}

func setBuildVersion(t *testing.T, v string) {
	t.Helper()
	orig := version.BuildVersion
	version.BuildVersion = v
	t.Cleanup(func() { version.BuildVersion = orig })
}

func TestUpdateCommand_CheckNoUpdate(t *testing.T) {
	currentTag := "cli/v0.1.0"
	releases := map[string]*githubRelease{
		currentTag: makeRelease(currentTag, false, currentPlatformAsset()),
	}
	srv := newTestServer(t, currentTag, releases)
	withTestServer(t, srv)
	setBuildVersion(t, "v0.1.0")

	cmd := New()
	cmd.SetArgs([]string{"--check"})
	var stdout strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&strings.Builder{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no update available")
	assert.Contains(t, stdout.String(), "already up to date")
}

func TestUpdateCommand_CheckUpdateAvailable(t *testing.T) {
	latestTag := "cli/v0.2.0"
	releases := map[string]*githubRelease{
		latestTag: makeRelease(latestTag, false, currentPlatformAsset()),
	}
	srv := newTestServer(t, latestTag, releases)
	withTestServer(t, srv)
	setBuildVersion(t, "v0.1.0")

	cmd := New()
	cmd.SetArgs([]string{"--check"})
	var stdout strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&strings.Builder{})

	err := cmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "Update available")
	assert.Contains(t, out, "0.2.0")
	assert.Contains(t, out, "Run 'ocm update' to install")
}

func TestUpdateCommand_DryRun(t *testing.T) {
	latestTag := "cli/v0.2.0"
	releases := map[string]*githubRelease{
		latestTag: makeRelease(latestTag, false, currentPlatformAsset()),
	}
	srv := newTestServer(t, latestTag, releases)
	withTestServer(t, srv)
	setBuildVersion(t, "v0.1.0")

	cmd := New()
	cmd.SetArgs([]string{"--dry-run", "--yes"})
	var stdout strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&strings.Builder{})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "[dry-run]")
}

func TestUpdateCommand_SpecificVersion(t *testing.T) {
	tag := "cli/v0.2.0"
	releases := map[string]*githubRelease{
		tag: makeRelease(tag, false, currentPlatformAsset()),
	}
	srv := newTestServer(t, tag, releases)
	withTestServer(t, srv)
	setBuildVersion(t, "v0.1.0")

	cmd := New()
	cmd.SetArgs([]string{"--version", "v0.2.0", "--dry-run", "--yes"})
	var stdout strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&strings.Builder{})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "[dry-run]")
	assert.Contains(t, stdout.String(), "0.2.0")
}

func TestUpdateCommand_PreReleaseIncluded(t *testing.T) {
	rcTag := "cli/v0.2.0-rc.1"
	releases := map[string]*githubRelease{
		rcTag: makeRelease(rcTag, true, currentPlatformAsset()),
	}
	srv := newTestServer(t, rcTag, releases)
	withTestServer(t, srv)
	setBuildVersion(t, "v0.1.0")

	cmd := New()
	cmd.SetArgs([]string{"--pre-release", "--check"})
	var stdout strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&strings.Builder{})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "0.2.0-rc.1")
}

// ---- filesystem tests ----

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "ocm")
	require.NoError(t, os.WriteFile(binaryPath, []byte("old content"), 0o755))

	tmpPath := filepath.Join(dir, "ocm-update-tmp")
	require.NoError(t, os.WriteFile(tmpPath, []byte("new content"), 0o755))

	require.NoError(t, replaceBinary(tmpPath, binaryPath))

	got, err := os.ReadFile(binaryPath)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(got))

	// Temp file should be gone (renamed away).
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "temp file should not exist after successful replace")

	// Backup file should be cleaned up.
	_, err = os.Stat(binaryPath + ".old")
	assert.True(t, os.IsNotExist(err), ".old backup should be cleaned up")
}

func TestReplaceBinary_PreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission tests are not meaningful on Windows")
	}
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "ocm")
	require.NoError(t, os.WriteFile(binaryPath, []byte("old"), 0o750))

	tmpPath := filepath.Join(dir, "ocm-update-tmp")
	require.NoError(t, os.WriteFile(tmpPath, []byte("new"), 0o644))

	require.NoError(t, replaceBinary(tmpPath, binaryPath))

	info, err := os.Stat(binaryPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm())
}
