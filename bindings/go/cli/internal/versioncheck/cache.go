package versioncheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	cacheSubDir = "ocm"
	cacheFile   = "version-check.json"
	// cacheTTL defines how long a cached GitHub API response is considered valid.
	// After this duration, the next CLI invocation will re-fetch from GitHub.
	cacheTTL = 24 * time.Hour
	// warnInterval defines the minimum time between showing upgrade warnings to the user.
	// Even if the cache indicates a newer version, the warning is suppressed until this
	// interval has elapsed since the last warning.
	warnInterval = 24 * time.Hour
)

// CacheEntry represents the persisted state of the version check.
type CacheEntry struct {
	// LatestVersion is the highest stable CLI release version found on GitHub (without "v" prefix).
	LatestVersion string `json:"latest_version"`
	// CheckedAt records when the GitHub API was last queried.
	CheckedAt time.Time `json:"checked_at"`
	// WarnedAt records when the user was last shown an upgrade warning.
	WarnedAt time.Time `json:"warned_at"`
}

// IsFresh returns true if the cache was populated recently enough to skip a network call.
func (c *CacheEntry) IsFresh(now time.Time) bool {
	return now.Sub(c.CheckedAt) < cacheTTL
}

// ShouldWarn returns true if enough time has passed since the last warning to show another one.
func (c *CacheEntry) ShouldWarn(now time.Time) bool {
	return now.Sub(c.WarnedAt) >= warnInterval
}

// CacheDir returns the platform-appropriate cache directory for OCM version check data.
// On Linux this is typically $XDG_CACHE_HOME/ocm or $HOME/.cache/ocm.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, cacheSubDir), nil
}

// CacheFilePath returns the full path to the version check cache file within the given directory.
func CacheFilePath(dir string) string {
	return filepath.Join(dir, cacheFile)
}

// ReadCache loads the cached version check state from disk.
// Returns an error if the file does not exist or contains invalid JSON.
func ReadCache(dir string) (*CacheEntry, error) {
	data, err := os.ReadFile(CacheFilePath(dir))
	if err != nil {
		return nil, err
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// WriteCache persists the version check state to disk, creating the cache directory if needed.
func WriteCache(dir string, entry *CacheEntry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(CacheFilePath(dir), data, 0o600)
}
