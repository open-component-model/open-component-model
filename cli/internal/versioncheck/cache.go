package versioncheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	cacheSubDir  = "ocm"
	cacheFile    = "version-check.json"
	cacheTTL     = 24 * time.Hour
	warnInterval = 24 * time.Hour
)

type CacheEntry struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
	WarnedAt      time.Time `json:"warned_at"`
}

func (c *CacheEntry) IsFresh(now time.Time) bool {
	return now.Sub(c.CheckedAt) < cacheTTL
}

func (c *CacheEntry) ShouldWarn(now time.Time) bool {
	return now.Sub(c.WarnedAt) >= warnInterval
}

func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, cacheSubDir), nil
}

func CacheFilePath(dir string) string {
	return filepath.Join(dir, cacheFile)
}

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

func WriteCache(dir string, entry *CacheEntry) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(CacheFilePath(dir), data, 0o644)
}
