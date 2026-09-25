package versioncheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheEntry_IsFresh(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		checkedAt time.Time
		want      bool
	}{
		{
			name:      "fresh cache",
			checkedAt: now.Add(-1 * time.Hour),
			want:      true,
		},
		{
			name:      "stale cache",
			checkedAt: now.Add(-25 * time.Hour),
			want:      false,
		},
		{
			name:      "exactly at boundary",
			checkedAt: now.Add(-24 * time.Hour),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &CacheEntry{CheckedAt: tt.checkedAt}
			if got := entry.IsFresh(now); got != tt.want {
				t.Errorf("IsFresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCacheEntry_ShouldWarn(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		warnedAt time.Time
		want     bool
	}{
		{
			name:     "never warned",
			warnedAt: time.Time{},
			want:     true,
		},
		{
			name:     "warned recently",
			warnedAt: now.Add(-1 * time.Hour),
			want:     false,
		},
		{
			name:     "warned long ago",
			warnedAt: now.Add(-25 * time.Hour),
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &CacheEntry{WarnedAt: tt.warnedAt}
			if got := entry.ShouldWarn(now); got != tt.want {
				t.Errorf("ShouldWarn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadWriteCache(t *testing.T) {
	dir := t.TempDir()

	entry := &CacheEntry{
		LatestVersion: "1.2.3",
		CheckedAt:     time.Now().Truncate(time.Second),
		WarnedAt:      time.Now().Add(-12 * time.Hour).Truncate(time.Second),
	}

	if err := WriteCache(dir, entry); err != nil {
		t.Fatalf("WriteCache() error = %v", err)
	}

	got, err := ReadCache(dir)
	if err != nil {
		t.Fatalf("ReadCache() error = %v", err)
	}

	if got.LatestVersion != entry.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", got.LatestVersion, entry.LatestVersion)
	}
	if !got.CheckedAt.Equal(entry.CheckedAt) {
		t.Errorf("CheckedAt = %v, want %v", got.CheckedAt, entry.CheckedAt)
	}
	if !got.WarnedAt.Equal(entry.WarnedAt) {
		t.Errorf("WarnedAt = %v, want %v", got.WarnedAt, entry.WarnedAt)
	}
}

func TestReadCache_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadCache(dir)
	if err == nil {
		t.Fatal("expected error for missing cache file")
	}
}

func TestReadCache_Corrupted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cacheFile), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadCache(dir)
	if err == nil {
		t.Fatal("expected error for corrupted cache file")
	}
}

func TestWriteCache_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")

	entry := &CacheEntry{LatestVersion: "1.0.0", CheckedAt: time.Now()}
	if err := WriteCache(dir, entry); err != nil {
		t.Fatalf("WriteCache() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, cacheFile)); err != nil {
		t.Fatalf("cache file not created: %v", err)
	}
}
