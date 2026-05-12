package setup

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/versioncheck"
)

const (
	// VersionCheckFlag is the name of the persistent flag controlling the version check.
	VersionCheckFlag = "version-check"
	// VersionCheckAuto enables the version check (default behavior).
	VersionCheckAuto = "auto"
	// VersionCheckDisable disables the version check entirely.
	VersionCheckDisable = "disable"
	// VersionCheckEnvVar is the environment variable that disables the version check when set to a truthy value.
	VersionCheckEnvVar = "OCM_DISABLE_VERSION_CHECK"
	// versionCheckWaitTimeout is how long cobra.OnFinalize waits for the async check to complete.
	// If the GitHub API hasn't responded by then, the warning is silently skipped.
	versionCheckWaitTimeout = 2 * time.Second
)

// RegisterVersionCheckFlag adds the --version-check persistent flag to the root command.
func RegisterVersionCheckFlag(cmd *cobra.Command) {
	enum.Var(cmd.PersistentFlags(), VersionCheckFlag,
		[]string{VersionCheckAuto, VersionCheckDisable},
		"control automatic version update checks")
}

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
				printWarning(result)
				versioncheck.MarkWarned(cacheDir)
			}
		case <-time.After(versionCheckWaitTimeout):
			slog.Debug("version check timed out waiting for result")
		}
	})
}

// isVersionCheckDisabled evaluates all opt-out mechanisms.
// Precedence: env var > CLI flag (if explicitly changed) > config policy.
func isVersionCheckDisabled(cmd *cobra.Command, currentVersion string) bool {
	// Dev builds have no meaningful version to compare.
	if currentVersion == "" || currentVersion == "n/a" {
		slog.Debug("version check skipped: no build version set")
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

	// CLI flag takes priority over config when explicitly set by the user.
	if flagChanged, flagVal := flagPolicy(cmd); flagChanged {
		if flagVal == VersionCheckDisable {
			slog.Debug("version check disabled via flag")
			return true
		}
		// Flag explicitly set to "auto" — override any config disable.
		return false
	}

	if configDisabled(cmd) {
		slog.Debug("version check disabled via config policy")
		return true
	}

	return false
}

// envDisabled checks the OCM_DISABLE_VERSION_CHECK environment variable.
func envDisabled() bool {
	val := os.Getenv(VersionCheckEnvVar)
	switch strings.ToLower(val) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// flagPolicy checks whether the --version-check flag was explicitly set by the user.
// Returns (true, value) if the flag was changed, (false, "") if it was left at default.
func flagPolicy(cmd *cobra.Command) (changed bool, value string) {
	flag := cmd.Flags().Lookup(VersionCheckFlag)
	if flag == nil {
		if root := cmd.Root(); root != nil {
			flag = root.PersistentFlags().Lookup(VersionCheckFlag)
		}
	}
	if flag == nil || !flag.Changed {
		return false, ""
	}
	return true, flag.Value.String()
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

// printWarning logs the upgrade notification using the structured logging stack.
func printWarning(result *versioncheck.Result) {
	slog.Warn("A newer version of ocm is available",
		slog.String("current", "v"+result.CurrentVersion),
		slog.String("available", "v"+result.LatestVersion),
		slog.String("url", fmt.Sprintf("https://github.com/%s/%s/releases/tag/%sv%s",
			versioncheck.DefaultGitHubOwner, versioncheck.DefaultGitHubRepo,
			versioncheck.DefaultTagPrefix, result.LatestVersion)),
	)
}
