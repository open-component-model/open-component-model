package integration

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/cmd/version"
	"ocm.software/open-component-model/bindings/go/cli/internal/versioncheck"
)

func Test_Integration_VersionCheck_FetchesLatestFromGitHub(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping GitHub API integration test in short mode")
	}
	r := require.New(t)
	t.Parallel()

	result := versioncheck.Check(t.Context(), versioncheck.Options{
		CurrentVersion: "0.0.1",
		CacheDir:       t.TempDir(),
		HTTPClient:     githubAPIClient(),
	})

	r.NotNil(result, "version check should succeed against real GitHub API")
	r.True(result.UpdateAvailable, "0.0.1 should be older than latest release")
	r.NotEmpty(result.LatestVersion)
}

func Test_Integration_VersionCheck_CurrentVersionIsLatest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping GitHub API integration test in short mode")
	}
	r := require.New(t)
	t.Parallel()

	result := versioncheck.Check(t.Context(), versioncheck.Options{
		CurrentVersion: "999.999.999",
		CacheDir:       t.TempDir(),
		HTTPClient:     githubAPIClient(),
	})

	r.NotNil(result)
	r.False(result.UpdateAvailable, "999.999.999 should not trigger update notification")
}

func Test_Integration_VersionCheck_DoesNotErrorWithOldVersion(t *testing.T) {
	r := require.New(t)

	origVersion := version.BuildVersion
	version.BuildVersion = "0.0.1"
	t.Cleanup(func() { version.BuildVersion = origVersion })

	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	cfgPath := writeEmptyConfig(t)
	rootCmd := cmd.New()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"--config", cfgPath, "version"})

	r.NoError(rootCmd.ExecuteContext(t.Context()))
}

func Test_Integration_VersionCheck_DisabledByEnvVar(t *testing.T) {
	r := require.New(t)

	t.Setenv("OCM_DISABLE_VERSION_CHECK", "1")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	origVersion := version.BuildVersion
	version.BuildVersion = "0.0.1"
	t.Cleanup(func() { version.BuildVersion = origVersion })

	cfgPath := writeEmptyConfig(t)
	rootCmd := cmd.New()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"--config", cfgPath, "version"})

	r.NoError(rootCmd.ExecuteContext(t.Context()))
}

func Test_Integration_VersionCheck_CachesResult(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping GitHub API integration test in short mode")
	}
	r := require.New(t)
	t.Parallel()

	cacheDir := t.TempDir()

	result1 := versioncheck.Check(t.Context(), versioncheck.Options{
		CurrentVersion: "0.0.1",
		CacheDir:       cacheDir,
		HTTPClient:     githubAPIClient(),
	})
	r.NotNil(result1)

	cache, err := versioncheck.ReadCache(cacheDir)
	r.NoError(err)
	r.NotEmpty(cache.LatestVersion)
	r.False(cache.CheckedAt.IsZero())
	firstCheckedAt := cache.CheckedAt

	// Second call should use the cache (not re-fetch).
	result2 := versioncheck.Check(t.Context(), versioncheck.Options{
		CurrentVersion: "0.0.1",
		CacheDir:       cacheDir,
		HTTPClient:     githubAPIClient(),
	})
	r.NotNil(result2)
	r.Equal(result1.LatestVersion, result2.LatestVersion, "second call should use cache")

	cache2, err := versioncheck.ReadCache(cacheDir)
	r.NoError(err)
	r.Equal(firstCheckedAt, cache2.CheckedAt, "cache timestamp should not change on cache hit")
}

// githubAPIClient returns a shared HTTP client for the live GitHub API used by these tests,
// built once via sync.Once. It differs from the production default for two reasons:
//   - Timeout: the production default (versioncheck.DefaultHTTPTimeout) is short for CLI
//     responsiveness, which is too short on a slow CI runner, so the tests time out.
//   - Auth: when GITHUB_TOKEN or GH_TOKEN is set (CI sets it), the client authenticates.
//     The unauthenticated GitHub API limit (60 requests/hour/IP) is shared across jobs on
//     GitHub-hosted runners and otherwise returns 403. Without a token the client stays
//     unauthenticated, so local runs are unchanged.
var (
	githubClientOnce sync.Once
	githubClient     *http.Client
)

func githubAPIClient() *http.Client {
	githubClientOnce.Do(func() {
		githubClient = &http.Client{Timeout: 30 * time.Second}
		if token := githubToken(); token != "" {
			githubClient.Transport = &bearerAuthTransport{token: token, base: http.DefaultTransport}
		}
	})
	return githubClient
}

// githubToken returns the first non-empty GitHub token from the environment, or "".
func githubToken() string {
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

// bearerAuthTransport adds a GitHub bearer token to each request without mutating the
// caller's request (the RoundTripper contract).
type bearerAuthTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Attach the token only to the GitHub API host. The http.Client strips sensitive headers
	// on cross-host redirects for the original request, but this transport adds the header
	// afterwards, so a redirect to another host would otherwise leak the token.
	if !strings.EqualFold(req.URL.Host, "api.github.com") {
		return t.base.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}

func writeEmptyConfig(t *testing.T) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	if err := os.WriteFile(cfgPath, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}
