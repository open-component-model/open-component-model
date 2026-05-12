// Package versioncheck provides non-blocking version update notifications for the OCM CLI.
//
// # Behavior
//
// On every CLI command invocation (except "version" itself), the package checks whether
// a newer stable release of the OCM CLI is available on GitHub. The check is designed to
// be invisible to the user under normal circumstances:
//
//   - The check runs asynchronously in a background goroutine and does not block command execution.
//   - Network errors, timeouts, and air-gapped environments are handled silently — no error is shown.
//   - Results are cached locally for 24 hours to avoid repeated network calls.
//   - The upgrade warning is rate-limited: it appears at most once every 24 hours.
//   - The warning is printed to stderr after command output completes, so it never interferes with stdout.
//
// # Release Filtering
//
// The OCM project is a monorepo with releases for multiple components. This package only
// considers releases tagged with the "cli/v" prefix (e.g. "cli/v0.5.0"). It excludes:
//
//   - Draft releases
//   - GitHub pre-releases (prerelease flag set)
//   - Semantic version pre-releases (e.g. "cli/v1.0.0-rc.1")
//
// # Opt-Out
//
// Users can disable the version check through two mechanisms (checked in priority order):
//
//  1. Environment variable: OCM_DISABLE_VERSION_CHECK=1 (also accepts "true" or "yes")
//  2. OCM config file: a versioncheck.cli.config.ocm.software/v1alpha1 entry with policy: disable
//
// # Cache Location
//
// The cache file is stored at $XDG_CACHE_HOME/ocm/version-check.json (or $HOME/.cache/ocm/
// on Linux when XDG_CACHE_HOME is unset). The cache directory is created automatically on
// first write.
package versioncheck
