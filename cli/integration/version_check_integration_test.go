package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/cmd/version"
	"ocm.software/open-component-model/cli/internal/versioncheck"
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
	})
	r.NotNil(result2)
	r.Equal(result1.LatestVersion, result2.LatestVersion, "second call should use cache")

	cache2, err := versioncheck.ReadCache(cacheDir)
	r.NoError(err)
	r.Equal(firstCheckedAt, cache2.CheckedAt, "cache timestamp should not change on cache hit")
}

func writeEmptyConfig(t *testing.T) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	if err := os.WriteFile(cfgPath, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}
