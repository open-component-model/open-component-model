package versioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	// DefaultGitHubOwner is the GitHub organization that owns the OCM repository.
	DefaultGitHubOwner = "open-component-model"
	// DefaultGitHubRepo is the GitHub repository name (monorepo containing CLI, controller, etc.).
	DefaultGitHubRepo = "open-component-model"
	// DefaultTagPrefix identifies CLI releases within the monorepo's release tags.
	DefaultTagPrefix = "cli/v"
	// DefaultHTTPTimeout is the maximum duration for the GitHub API request.
	// Kept short to avoid delaying CLI commands in degraded network conditions.
	DefaultHTTPTimeout = 5 * time.Second
	// releasesPerPage is the number of releases to fetch in a single API call.
	// Since releases are ordered by creation date descending, the latest stable
	// CLI release is typically within the first page.
	releasesPerPage = 20
)

// Options configures the version check behavior.
type Options struct {
	// CurrentVersion is the semantic version of the running binary (set via ldflags at build time).
	CurrentVersion string
	// CacheDir overrides the default cache directory. If empty, CacheDir() is used.
	CacheDir string
	// GitHubOwner is the repository owner. Defaults to DefaultGitHubOwner.
	GitHubOwner string
	// GitHubRepo is the repository name. Defaults to DefaultGitHubRepo.
	GitHubRepo string
	// TagPrefix filters release tags. Only tags starting with this prefix are considered.
	TagPrefix string
	// HTTPClient allows injecting a custom HTTP client (useful for testing).
	HTTPClient *http.Client
	// BaseURL overrides the GitHub API base URL (useful for testing with httptest).
	BaseURL string
}

func (o *Options) defaults() {
	if o.GitHubOwner == "" {
		o.GitHubOwner = DefaultGitHubOwner
	}
	if o.GitHubRepo == "" {
		o.GitHubRepo = DefaultGitHubRepo
	}
	if o.TagPrefix == "" {
		o.TagPrefix = DefaultTagPrefix
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	if o.BaseURL == "" {
		o.BaseURL = "https://api.github.com"
	}
}

// Result holds the outcome of a version comparison.
type Result struct {
	// CurrentVersion is the parsed version of the running binary.
	CurrentVersion string
	// LatestVersion is the highest stable release found on GitHub.
	LatestVersion string
	// UpdateAvailable is true when LatestVersion is newer than CurrentVersion.
	UpdateAvailable bool
}

// Check performs a version check, using the local cache when possible.
// Returns nil on any error (network failure, parse error, etc.) to ensure silent failure.
// The caller should treat a nil result as "no update information available".
func Check(ctx context.Context, opts Options) *Result {
	opts.defaults()

	current, err := semver.NewVersion(opts.CurrentVersion)
	if err != nil {
		return nil
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir, err = CacheDir()
		if err != nil {
			return nil
		}
	}

	now := time.Now()

	// Use cached result if the last check was recent enough.
	cache, err := ReadCache(cacheDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("version check: cache read failed", slog.String("error", err.Error()))
	}
	if cache != nil && cache.IsFresh(now) {
		return compareVersions(current, cache.LatestVersion)
	}

	// Cache is stale or missing — fetch from GitHub.
	latest, err := fetchLatestVersion(ctx, opts)
	if err != nil {
		slog.Debug("version check: fetch failed", slog.String("error", err.Error()))
		// Cache the failure so we don't retry on every CLI invocation (respects cacheTTL).
		failEntry := &CacheEntry{CheckedAt: now}
		if cache != nil {
			failEntry.LatestVersion = cache.LatestVersion
			failEntry.WarnedAt = cache.WarnedAt
		}
		if writeErr := WriteCache(cacheDir, failEntry); writeErr != nil {
			slog.Debug("version check: failed to write cache", slog.String("error", writeErr.Error()))
		}
		return nil
	}

	// Persist the new result, preserving the previous WarnedAt timestamp.
	entry := &CacheEntry{
		LatestVersion: latest,
		CheckedAt:     now,
	}
	if cache != nil {
		entry.WarnedAt = cache.WarnedAt
	}
	if err := WriteCache(cacheDir, entry); err != nil {
		slog.Debug("version check: failed to write cache", slog.String("error", err.Error()))
	}

	return compareVersions(current, latest)
}

// MarkWarned updates the cache to record that an upgrade warning was just shown.
// This prevents the warning from appearing again until warnInterval has elapsed.
func MarkWarned(cacheDir string) {
	cache, _ := ReadCache(cacheDir)
	if cache == nil {
		return
	}
	cache.WarnedAt = time.Now()
	if err := WriteCache(cacheDir, cache); err != nil {
		slog.Debug("version check: failed to persist warned_at", slog.String("error", err.Error()))
	}
}

// compareVersions returns a Result comparing the current version against a latest version string.
// Returns nil if the latest version string cannot be parsed.
func compareVersions(current *semver.Version, latestStr string) *Result {
	latest, err := semver.NewVersion(latestStr)
	if err != nil {
		return nil
	}
	return &Result{
		CurrentVersion:  current.String(),
		LatestVersion:   latest.String(),
		UpdateAvailable: current.LessThan(latest),
	}
}

// githubRelease is a minimal representation of the GitHub Releases API response.
type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// fetchLatestVersion queries the GitHub Releases API and returns the highest stable CLI version.
// It filters out drafts, pre-releases, non-CLI tags, and semver pre-release versions.
func fetchLatestVersion(ctx context.Context, opts Options) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d",
		opts.BaseURL, opts.GitHubOwner, opts.GitHubRepo, releasesPerPage)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	// Limit response body to 1MB to prevent excessive memory use on unexpected responses.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", err
	}

	var best *semver.Version
	for _, r := range releases {
		// GitHub API does not support server-side filtering by draft/prerelease status
		// for the list endpoint. The /releases/latest endpoint excludes these but doesn't
		// support tag prefix filtering needed for monorepo releases.
		if r.Draft || r.Prerelease {
			continue
		}
		if !strings.HasPrefix(r.TagName, opts.TagPrefix) {
			continue
		}
		vStr := strings.TrimPrefix(r.TagName, opts.TagPrefix)
		v, err := semver.NewVersion(vStr)
		if err != nil {
			continue
		}
		// Double-check: exclude semver pre-releases that GitHub may not flag.
		if v.Prerelease() != "" {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
		}
	}

	if best == nil {
		return "", fmt.Errorf("no stable cli release found")
	}

	return best.String(), nil
}
