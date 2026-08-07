package versioncheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheck_NewerVersionAvailable(t *testing.T) {
	releases := []githubRelease{
		{TagName: "cli/v1.2.0", Draft: false, Prerelease: false},
		{TagName: "cli/v1.1.0", Draft: false, Prerelease: false},
	}
	srv := newTestServer(t, releases)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.UpdateAvailable {
		t.Error("expected UpdateAvailable = true")
	}
	if result.LatestVersion != "1.2.0" {
		t.Errorf("LatestVersion = %q, want %q", result.LatestVersion, "1.2.0")
	}
}

func TestCheck_CurrentIsLatest(t *testing.T) {
	releases := []githubRelease{
		{TagName: "cli/v1.0.0", Draft: false, Prerelease: false},
	}
	srv := newTestServer(t, releases)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.UpdateAvailable {
		t.Error("expected UpdateAvailable = false")
	}
}

func TestCheck_SkipsPreReleases(t *testing.T) {
	releases := []githubRelease{
		{TagName: "cli/v2.0.0-rc.1", Draft: false, Prerelease: true},
		{TagName: "cli/v1.1.0", Draft: false, Prerelease: false},
	}
	srv := newTestServer(t, releases)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.LatestVersion != "1.1.0" {
		t.Errorf("LatestVersion = %q, want %q (should skip prerelease)", result.LatestVersion, "1.1.0")
	}
}

func TestCheck_SkipsDrafts(t *testing.T) {
	releases := []githubRelease{
		{TagName: "cli/v2.0.0", Draft: true, Prerelease: false},
		{TagName: "cli/v1.1.0", Draft: false, Prerelease: false},
	}
	srv := newTestServer(t, releases)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.LatestVersion != "1.1.0" {
		t.Errorf("LatestVersion = %q, want %q (should skip draft)", result.LatestVersion, "1.1.0")
	}
}

func TestCheck_OnlyConsidersCLITags(t *testing.T) {
	releases := []githubRelease{
		{TagName: "kubernetes/controller/v5.0.0", Draft: false, Prerelease: false},
		{TagName: "cli/v1.1.0", Draft: false, Prerelease: false},
	}
	srv := newTestServer(t, releases)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.LatestVersion != "1.1.0" {
		t.Errorf("LatestVersion = %q, want %q", result.LatestVersion, "1.1.0")
	}
}

func TestCheck_SkipsSemverPrerelease(t *testing.T) {
	releases := []githubRelease{
		{TagName: "cli/v2.0.0-beta.1", Draft: false, Prerelease: false},
		{TagName: "cli/v1.1.0", Draft: false, Prerelease: false},
	}
	srv := newTestServer(t, releases)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.LatestVersion != "1.1.0" {
		t.Errorf("LatestVersion = %q, want %q (should skip semver prerelease)", result.LatestVersion, "1.1.0")
	}
}

func TestCheck_NetworkError_ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result != nil {
		t.Error("expected nil result on network error")
	}
}

func TestCheck_InvalidCurrentVersion_ReturnsNil(t *testing.T) {
	result := Check(t.Context(), Options{
		CurrentVersion: "not-a-version",
		CacheDir:       t.TempDir(),
	})

	if result != nil {
		t.Error("expected nil result for invalid current version")
	}
}

func TestCheck_NAVersion_ReturnsNil(t *testing.T) {
	result := Check(t.Context(), Options{
		CurrentVersion: "n/a",
		CacheDir:       t.TempDir(),
	})

	if result != nil {
		t.Error("expected nil result for n/a version")
	}
}

func TestCheck_UsesCachedResult(t *testing.T) {
	dir := t.TempDir()

	entry := &CacheEntry{
		LatestVersion: "2.0.0",
		CheckedAt:     time.Now(),
	}
	if err := WriteCache(dir, entry); err != nil {
		t.Fatal(err)
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]githubRelease{})
	}))
	t.Cleanup(srv.Close)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       dir,
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if callCount != 0 {
		t.Errorf("expected 0 HTTP calls when cache is fresh, got %d", callCount)
	}
	if result == nil {
		t.Fatal("expected non-nil result from cache")
	}
	if result.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q (from cache)", result.LatestVersion, "2.0.0")
	}
}

func TestCheck_ExpiredCache_FetchesFromNetwork(t *testing.T) {
	dir := t.TempDir()

	entry := &CacheEntry{
		LatestVersion: "1.0.0",
		CheckedAt:     time.Now().Add(-25 * time.Hour),
	}
	if err := WriteCache(dir, entry); err != nil {
		t.Fatal(err)
	}

	releases := []githubRelease{
		{TagName: "cli/v2.0.0", Draft: false, Prerelease: false},
	}
	srv := newTestServer(t, releases)

	result := Check(t.Context(), Options{
		CurrentVersion: "1.0.0",
		CacheDir:       dir,
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q (fresh fetch)", result.LatestVersion, "2.0.0")
	}
}

func TestCheck_Timeout_ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	result := Check(ctx, Options{
		CurrentVersion: "1.0.0",
		CacheDir:       t.TempDir(),
		HTTPClient:     srv.Client(),
		BaseURL:        srv.URL,
		GitHubOwner:    "test",
		GitHubRepo:     "test",
		TagPrefix:      "cli/v",
	})

	if result != nil {
		t.Error("expected nil result on timeout")
	}
}

func TestMarkWarned(t *testing.T) {
	dir := t.TempDir()

	entry := &CacheEntry{
		LatestVersion: "2.0.0",
		CheckedAt:     time.Now(),
	}
	if err := WriteCache(dir, entry); err != nil {
		t.Fatal(err)
	}

	MarkWarned(dir)

	got, err := ReadCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.WarnedAt.IsZero() {
		t.Error("expected WarnedAt to be set after MarkWarned")
	}
	if time.Since(got.WarnedAt) > time.Second {
		t.Error("WarnedAt should be recent")
	}
}

func newTestServer(t *testing.T, releases []githubRelease) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(releases)
	}))
	t.Cleanup(srv.Close)
	return srv
}
