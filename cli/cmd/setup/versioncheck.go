package setup

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/versioncheck"
)

const (
	VersionCheckFlag        = "version-check"
	VersionCheckAuto        = "auto"
	VersionCheckDisable     = "disable"
	VersionCheckEnvVar      = "OCM_DISABLE_VERSION_CHECK"
	versionCheckWaitTimeout = 2 * time.Second
)

func RegisterVersionCheckFlag(cmd *cobra.Command) {
	enum.Var(cmd.PersistentFlags(), VersionCheckFlag,
		[]string{VersionCheckAuto, VersionCheckDisable},
		"control automatic version update checks")
}

func VersionCheck(cmd *cobra.Command, currentVersion string) {
	if isVersionCheckDisabled(cmd, currentVersion) {
		return
	}

	cacheDir, err := versioncheck.CacheDir()
	if err != nil {
		slog.Debug("version check skipped: cannot determine cache directory", slog.String("error", err.Error()))
		return
	}

	cache, _ := versioncheck.ReadCache(cacheDir)
	if cache != nil && !cache.ShouldWarn(time.Now()) {
		slog.Debug("version check skipped: warned recently")
		return
	}

	ch := make(chan *versioncheck.Result, 1)
	go func() {
		result := versioncheck.Check(cmd.Context(), versioncheck.Options{
			CurrentVersion: currentVersion,
			CacheDir:       cacheDir,
		})
		ch <- result
	}()

	cobra.OnFinalize(func() {
		select {
		case result := <-ch:
			if result != nil && result.UpdateAvailable {
				printWarning(cmd.ErrOrStderr(), result)
				versioncheck.MarkWarned(cacheDir)
			}
		case <-time.After(versionCheckWaitTimeout):
			slog.Debug("version check timed out waiting for result")
		}
	})
}

func isVersionCheckDisabled(cmd *cobra.Command, currentVersion string) bool {
	if currentVersion == "" || currentVersion == "n/a" {
		slog.Debug("version check skipped: no build version set")
		return true
	}

	if cmd.Name() == "version" {
		return true
	}

	if envDisabled() {
		slog.Debug("version check disabled via environment variable")
		return true
	}

	if flagDisabled(cmd) {
		slog.Debug("version check disabled via flag")
		return true
	}

	return false
}

func envDisabled() bool {
	val := os.Getenv(VersionCheckEnvVar)
	switch strings.ToLower(val) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func flagDisabled(cmd *cobra.Command) bool {
	val, err := enum.Get(cmd.Flags(), VersionCheckFlag)
	if err != nil {
		root := cmd.Root()
		if root != nil {
			val, err = enum.Get(root.PersistentFlags(), VersionCheckFlag)
		}
		if err != nil {
			return false
		}
	}
	return val == VersionCheckDisable
}

func printWarning(w io.Writer, result *versioncheck.Result) {
	fmt.Fprintf(w, "\nA newer version of ocm is available: v%s (current: v%s)\n", result.LatestVersion, result.CurrentVersion)
	fmt.Fprintf(w, "See: https://github.com/%s/%s/releases/tag/%sv%s\n",
		versioncheck.DefaultGitHubOwner, versioncheck.DefaultGitHubRepo,
		versioncheck.DefaultTagPrefix, result.LatestVersion)
}
