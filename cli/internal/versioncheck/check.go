package versioncheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	DefaultGitHubOwner = "open-component-model"
	DefaultGitHubRepo  = "open-component-model"
	DefaultTagPrefix   = "cli/v"
	DefaultHTTPTimeout = 5 * time.Second
	releasesPerPage    = 20
)

type Options struct {
	CurrentVersion string
	CacheDir       string
	GitHubOwner    string
	GitHubRepo     string
	TagPrefix      string
	HTTPClient     *http.Client
	BaseURL        string
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

type Result struct {
	CurrentVersion string
	LatestVersion  string
	UpdateAvailable bool
}

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

	cache, _ := ReadCache(cacheDir)
	if cache != nil && cache.IsFresh(now) {
		return compareVersions(current, cache.LatestVersion)
	}

	latest, err := fetchLatestVersion(ctx, opts)
	if err != nil {
		return nil
	}

	entry := &CacheEntry{
		LatestVersion: latest,
		CheckedAt:     now,
	}
	if cache != nil {
		entry.WarnedAt = cache.WarnedAt
	}
	_ = WriteCache(cacheDir, entry)

	return compareVersions(current, latest)
}

func MarkWarned(cacheDir string) {
	cache, _ := ReadCache(cacheDir)
	if cache == nil {
		return
	}
	cache.WarnedAt = time.Now()
	_ = WriteCache(cacheDir, cache)
}

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

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

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
