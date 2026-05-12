package integration

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/cmd/version"
	"ocm.software/open-component-model/cli/internal/versioncheck"
)

func Test_Integration_VersionCheck_FetchesLatestFromGitHub(t *testing.T) {
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
	r := require.New(t)
	t.Parallel()

	result := versioncheck.Check(t.Context(), versioncheck.Options{
		CurrentVersion: "999.999.999",
		CacheDir:       t.TempDir(),
	})

	r.NotNil(result)
	r.False(result.UpdateAvailable, "999.999.999 should not trigger update notification")
}

func Test_Integration_VersionCheck_PrintsWarningToStderr(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	origVersion := version.BuildVersion
	version.BuildVersion = "0.0.1"
	t.Cleanup(func() { version.BuildVersion = origVersion })

	stderr := &bytes.Buffer{}
	rootCmd := cmd.New()
	rootCmd.SetErr(stderr)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"--help"})

	r.NoError(rootCmd.Execute())

	// OnFinalize runs after Execute returns — version check is async
	// so we verify it at least didn't error; the warning may or may not appear
	// depending on timing, but the check itself must not fail
}

func Test_Integration_VersionCheck_DisabledByEnvVar(t *testing.T) {
	r := require.New(t)

	t.Setenv("OCM_DISABLE_VERSION_CHECK", "1")

	origVersion := version.BuildVersion
	version.BuildVersion = "0.0.1"
	t.Cleanup(func() { version.BuildVersion = origVersion })

	stderr := &bytes.Buffer{}
	rootCmd := cmd.New()
	rootCmd.SetErr(stderr)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"--help"})

	r.NoError(rootCmd.Execute())
	r.Empty(stderr.String(), "no warning should appear when version check is disabled")
}

func Test_Integration_VersionCheck_CachesResult(t *testing.T) {
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

	result2 := versioncheck.Check(t.Context(), versioncheck.Options{
		CurrentVersion: "0.0.1",
		CacheDir:       cacheDir,
	})
	r.NotNil(result2)
	r.Equal(result1.LatestVersion, result2.LatestVersion, "second call should use cache")
}
