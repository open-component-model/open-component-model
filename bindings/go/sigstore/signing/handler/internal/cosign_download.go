package internal

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "embed"
)

//go:embed .env
var envFile string

// cosignVersion is the pinned cosign version for auto-download, parsed from .env.
var cosignVersion = parseCosignVersion()

// platformBinaryName is the platform-specific cosign binary name for the current OS/arch.
var platformBinaryName = cosignBinaryName(runtime.GOOS, runtime.GOARCH)

// pinnedDownloadURL is the resolved download URL for the pinned cosign version and current platform.
var pinnedDownloadURL = cosignDownloadURL(cosignVersion, runtime.GOOS, runtime.GOARCH)

// pinnedChecksumsURL is the resolved checksums URL for the pinned cosign version.
var pinnedChecksumsURL = fmt.Sprintf(cosignChecksumsURLFmt, cosignVersion)

func parseCosignVersion() string {
	for _, line := range strings.Split(envFile, "\n") {
		if v, ok := strings.CutPrefix(line, "COSIGN_VERSION="); ok {
			return strings.TrimSpace(v)
		}
	}
	panic("COSIGN_VERSION not found in .env")
}

const cosignChecksumsURLFmt = "https://github.com/sigstore/cosign/releases/download/%s/cosign_checksums.txt"

// cosignDownloadURL returns the GitHub release download URL for the given
// cosign version and platform.
func cosignDownloadURL(version, goos, goarch string) string {
	return fmt.Sprintf("https://github.com/sigstore/cosign/releases/download/%s/%s", version, cosignBinaryName(goos, goarch))
}

// cosignBinaryName returns the platform-specific binary name.
func cosignBinaryName(goos, goarch string) string {
	name := fmt.Sprintf("cosign-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// cosignCachePath returns the directory where the versioned cosign binary is stored.
func cosignCachePath(version string) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("determine user cache directory: %w", err)
	}
	return filepath.Join(cacheDir, "ocm", "cosign", version), nil
}

// ensureOrDownloadCosign returns a path to a usable cosign binary.
// It checks the cache first, then downloads if necessary with checksum verification.
// This function is not safe for concurrent use; the caller must serialize access
// (DefaultExecutor.ensureCosignAvailable holds its mutex before calling this).
func ensureOrDownloadCosign(ctx context.Context, client *http.Client) (string, error) {
	cacheDir, err := cosignCachePath(cosignVersion)
	if err != nil {
		return "", err
	}

	cachedPath := filepath.Join(cacheDir, platformBinaryName)

	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("create cache directory %s: %w", cacheDir, err)
	}

	info, err := os.Lstat(cachedPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No cache — fall through to download.
	case err != nil:
		return "", fmt.Errorf("stat cached binary %s: %w", cachedPath, err)
	case info.Mode().IsRegular():
		if savedHash, err := os.ReadFile(cachedPath + ".sha256"); err == nil {
			if err := verifyFileChecksum(cachedPath, strings.TrimSpace(string(savedHash))); err == nil {
				return cachedPath, nil
			} else {
				slog.Warn("cached cosign binary failed checksum verification; possible binary tampering — re-downloading",
					"path", cachedPath,
					"expected_hash", strings.TrimSpace(string(savedHash)),
					"error", err,
				)
			}
		} else {
			slog.Debug("cached cosign binary found but checksum sidecar missing; re-downloading",
				"binary", cachedPath, "error", err)
		}
	}

	expectedHash, err := fetchExpectedChecksumWith(ctx, client, platformBinaryName)
	if err != nil {
		return "", fmt.Errorf("fetch checksums: %w", err)
	}

	binaryURL := pinnedDownloadURL
	if err := downloadAndVerifyWith(ctx, client, binaryURL, cachedPath, expectedHash); err != nil {
		return "", err
	}

	return cachedPath, nil
}

const (
	maxBinaryDownloadSize   = 150 << 20 // 150 MB safety cap
	maxChecksumDownloadSize = 1 << 20   // 1 MB
	userAgent               = "ocm.software/open-component-model/bindings/go/sigstore"
)

// fetchExpectedChecksumWith downloads cosign_checksums.txt for the pinned version
// and returns the SHA256 hash for the specified binary name.
// The checksum guards against download corruption and partial CDN cache poisoning.
// Full Sigstore bundle verification of the release is not implemented: verifying
// the bundle requires cosign itself (chicken-and-egg for an auto-download fallback).
func fetchExpectedChecksumWith(ctx context.Context, client *http.Client, binaryName string) (result string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pinnedChecksumsURL, nil)
	if err != nil {
		return "", fmt.Errorf("create checksums request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download checksums file: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksums file: HTTP %d", resp.StatusCode)
	}

	return parseChecksum(io.LimitReader(resp.Body, maxChecksumDownloadSize), binaryName)
}

// parseChecksum reads a checksums file (format: "<hash>  <filename>\n") and
// returns the hash for the given filename.
func parseChecksum(r io.Reader, filename string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == filename {
			return parts[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return "", fmt.Errorf("checksum not found for %s", filename)
}

// verifyFileChecksum checks that the SHA256 hash of a file matches the expected value.
func verifyFileChecksum(path, expectedHash string) (err error) {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}

	if hex.EncodeToString(hasher.Sum(nil)) != expectedHash {
		return fmt.Errorf("checksum mismatch for %s", path)
	}
	return nil
}

// downloadAndVerifyWith downloads the binary from url, verifies its SHA256 against
// expectedHash, writes it to destPath, and makes it executable.
func downloadAndVerifyWith(ctx context.Context, client *http.Client, url, destPath, expectedHash string) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download cosign binary: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download cosign binary: HTTP %d from %s", resp.StatusCode, url)
	}

	if resp.ContentLength > maxBinaryDownloadSize {
		return fmt.Errorf("cosign binary too large: Content-Length %d exceeds safety limit %d",
			resp.ContentLength, maxBinaryDownloadSize)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), "cosign-download-*")
	if err != nil {
		return fmt.Errorf("create temp download file: %w", err)
	}
	tmpPath := tmpFile.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	n, err := io.CopyN(writer, resp.Body, maxBinaryDownloadSize+1)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = tmpFile.Close()
		return fmt.Errorf("download cosign binary: %w", err)
	}
	if n > maxBinaryDownloadSize {
		_ = tmpFile.Close()
		return fmt.Errorf("cosign binary download exceeded size limit of %d bytes", maxBinaryDownloadSize)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("flush cosign binary download: %w", err)
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("cosign binary checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	if err := os.Chmod(tmpPath, 0o700); err != nil {
		return fmt.Errorf("make cosign binary executable: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("move cosign binary to cache: %w", err)
	}
	renamed = true

	if err := os.WriteFile(destPath+".sha256", []byte(expectedHash), 0o600); err != nil {
		slog.Warn("failed to write checksum sidecar (binary will be re-verified on next use)",
			"path", destPath+".sha256",
			"error", err)
	}

	return nil
}
