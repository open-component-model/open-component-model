package setup

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/versioncheck"
)

const (
	// VersionCheckEnvVar is the environment variable that disables the version check when set to a truthy value.
	VersionCheckEnvVar = "OCM_DISABLE_VERSION_CHECK"
	// versionCheckWaitTimeout is how long cobra.OnFinalize waits for the async check to complete.
	// If the GitHub API hasn't responded by then, the warning is silently skipped.
	versionCheckWaitTimeout = 2 * time.Second
)

// VersionCheck starts an asynchronous version check and registers a cobra.OnFinalize callback
// to print the upgrade warning (if applicable) after command execution completes.
//
// The check is skipped entirely when any opt-out mechanism is active or the binary has no
// build version set. This function is called from PersistentPreRunE, so it runs before
// every command.
func VersionCheck(cmd *cobra.Command, currentVersion string) {
	if isVersionCheckDisabled(cmd, currentVersion) {
		return
	}

	cacheDir, err := versioncheck.CacheDir()
	if err != nil {
		slog.Debug("version check skipped: cannot determine cache directory", slog.String("error", err.Error()))
		return
	}

	// Skip early if we already warned the user recently — avoids spawning a goroutine.
	cache, _ := versioncheck.ReadCache(cacheDir)
	if cache != nil && !cache.ShouldWarn(time.Now()) {
		slog.Debug("version check skipped: warned recently")
		return
	}

	// Run the actual check in a background goroutine so it doesn't block command execution.
	ch := make(chan *versioncheck.Result, 1)
	go func() {
		result := versioncheck.Check(cmd.Context(), versioncheck.Options{
			CurrentVersion: currentVersion,
			CacheDir:       cacheDir,
		})
		ch <- result
	}()

	// Print the warning after the command finishes, so it doesn't interleave with command output.
	cobra.OnFinalize(func() {
		select {
		case result := <-ch:
			if result != nil && result.UpdateAvailable {
				printUpgradeWarning(result)
				versioncheck.MarkWarned(cacheDir)
			}
		case <-time.After(versionCheckWaitTimeout):
			slog.Debug("version check timed out waiting for result")
		}
	})
}

// isVersionCheckDisabled evaluates opt-out mechanisms.
// Precedence: env var > config policy.
func isVersionCheckDisabled(cmd *cobra.Command, currentVersion string) bool {
	// Dev builds have no meaningful version to compare.
	if currentVersion == "" || currentVersion == "n/a" {
		slog.Debug("version check skipped: no build version set")
		return true
	}

	// Pseudo-versions (e.g. 0.0.0-20260520062203-abc) are produced by go install from
	// an untagged commit. They are not real releases and must not trigger a warning.
	// RCs and alphas (e.g. 0.6.0-rc.1) are intentional pre-releases and should warn.
	if v, err := semver.NewVersion(currentVersion); err != nil ||
		(v.Major() == 0 && v.Minor() == 0 && v.Patch() == 0 && v.Prerelease() != "") {
		slog.Debug("version check skipped: dev/pseudo-version build", slog.String("version", currentVersion))
		return true
	}

	// Don't show an upgrade warning alongside version output — redundant.
	if cmd.Name() == "version" {
		return true
	}

	if envDisabled() {
		slog.Debug("version check disabled via environment variable")
		return true
	}

	if configDisabled(cmd) {
		slog.Debug("version check disabled via config policy")
		return true
	}

	return false
}

// envDisabled checks the OCM_DISABLE_VERSION_CHECK environment variable.
// Recognizes strconv.ParseBool values (1/t/true/0/f/false). Unrecognized non-empty
// values are treated as "disabled" for safety.
func envDisabled() bool {
	val := os.Getenv(VersionCheckEnvVar)
	if val == "" {
		return false
	}
	disabled, err := strconv.ParseBool(val)
	if err != nil {
		return true
	}
	return disabled
}

// configDisabled checks the OCM config file for a versioncheck policy.
func configDisabled(cmd *cobra.Command) bool {
	ctx := ocmctx.FromContext(cmd.Context())
	if ctx == nil {
		return false
	}
	cfg := ctx.Configuration()
	if cfg == nil {
		return false
	}
	vcCfg, err := versioncheck.LookupConfig(cfg)
	if err != nil {
		slog.Debug("version check config lookup failed", slog.String("error", err.Error()))
		return false
	}
	return vcCfg.Policy == versioncheck.PolicyDisable
}

// printUpgradeWarning logs the upgrade notification using the structured logging stack.
func printUpgradeWarning(result *versioncheck.Result) {
	slog.Warn("A newer version of ocm is available",
		slog.String("current", "v"+result.CurrentVersion),
		slog.String("available", "v"+result.LatestVersion),
		slog.String("url", fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s%s",
			versioncheck.DefaultGitHubOwner, versioncheck.DefaultGitHubRepo,
			versioncheck.DefaultTagPrefix, result.LatestVersion)),
	)
}
