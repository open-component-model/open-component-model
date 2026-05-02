package handler

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "embed"
)

//go:embed .env
var envFile string

// CosignVersion is the pinned cosign version for auto-download, parsed from .env.
var CosignVersion = parseCosignVersion()

func parseCosignVersion() string {
	for _, line := range strings.Split(envFile, "\n") {
		if v, ok := strings.CutPrefix(line, "COSIGN_VERSION="); ok {
			return strings.TrimSpace(v)
		}
	}
	panic("COSIGN_VERSION not found in .env")
}

// cosignDownloadURL returns the GitHub release download URL for the given
// cosign version and platform.
func cosignDownloadURL(version, goos, goarch string) string {
	return fmt.Sprintf("https://github.com/sigstore/cosign/releases/download/%s/%s", version, cosignBinaryName(goos, goarch))
}

// cosignChecksumsURL returns the URL for the checksums file of the given version.
func cosignChecksumsURL(version string) string {
	return fmt.Sprintf("https://github.com/sigstore/cosign/releases/download/%s/cosign_checksums.txt", version)
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
//
// Concurrency: Multiple goroutines (or processes) may call this concurrently.
// The final os.Rename is atomic on POSIX, so concurrent downloads produce a valid
// binary but waste bandwidth. Within a single DefaultExecutor instance, the mutex
// and resolved flag ensure at most one resolution attempt per process (with retry
// on transient failure).
// TODO(controller): add flock-based locking when concurrent controller access is needed.
//
// Security note (TOCTOU): The cache check (Stat) and binary execution are two
// separate non-atomic steps. On a shared machine, a malicious local user with
// write access to the cache directory could replace the verified binary between
// Stat and exec. The cache directory is created with 0o700 permissions (owner-only)
// which eliminates this risk for the common case. On network filesystems or when
// running as root, additional hardening (e.g. executing from an fd rather than
// a path) would be needed.
func ensureOrDownloadCosign(ctx context.Context) (string, error) {
	cacheDir, err := cosignCachePath(CosignVersion)
	if err != nil {
		return "", err
	}

	binaryName := cosignBinaryName(runtime.GOOS, runtime.GOARCH)
	cachedPath := filepath.Join(cacheDir, binaryName)

	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("create cache directory %s: %w", cacheDir, err)
	}

	if info, err := os.Lstat(cachedPath); err == nil && info.Mode().IsRegular() {
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

	expectedHash, err := fetchExpectedChecksum(ctx, CosignVersion, binaryName)
	if err != nil {
		return "", fmt.Errorf("fetch checksums: %w", err)
	}

	binaryURL := cosignDownloadURL(CosignVersion, runtime.GOOS, runtime.GOARCH)
	if err := downloadAndVerify(ctx, binaryURL, cachedPath, expectedHash); err != nil {
		return "", err
	}

	return cachedPath, nil
}

// downloadHTTPClient is used for all cosign download HTTP requests.
var downloadHTTPClient = &http.Client{Timeout: 2 * time.Minute}

const (
	maxBinaryDownloadSize   = 150 << 20 // 150 MB safety cap
	maxChecksumDownloadSize = 1 << 20   // 1 MB
)

// fetchExpectedChecksum downloads the cosign_checksums.txt for the given version
// and returns the SHA256 hash for the specified binary name.
//
// Security note (threat model): Both the binary and the checksums file are
// fetched over HTTPS from the same GitHub release. Go's net/http validates the
// TLS certificate against the OS trust store, so an attacker needs a trusted CA
// (or local TLS proxy) to intercept the connection. Given that baseline, the
// SHA256 check guards against:
//   - accidental data corruption or truncation during download,
//   - partial CDN cache poisoning where only one artefact is stale.
//
// It does NOT guard against a fully compromised GitHub release where binary and
// checksums are replaced atomically. That scenario also compromises the Sigstore
// bundle shipped alongside the release (cosign_checksums.txt.sigstore.json),
// because the attacker controls the signing workflow that produces it. Full
// Sigstore bundle verification (cert chain + Rekor inclusion proof + RFC 3161
// timestamps) would add defence-in-depth, but the complexity (~300-800 lines of
// hand-rolled stdlib crypto to preserve the zero-sigstore-deps constraint) is
// disproportionate for a convenience auto-download fallback.
//
// TODO(ocm-project#996): Consider a lightweight identity check as an incremental
// improvement. Download cosign_checksums.txt.sigstore.json, parse the Fulcio leaf
// certificate (stdlib crypto/x509 + encoding/json, ~100 lines), and verify the
// SAN matches the expected GitHub Actions workflow identity
// (sigstore/cosign/.github/workflows/release.yml). This does not verify the
// cryptographic chain but confirms the bundle claims the expected signer, which
// catches accidental mis-signing and adds a meaningful signal without a full
// hand-rolled Sigstore verifier.
func fetchExpectedChecksum(ctx context.Context, version, binaryName string) (string, error) {
	return fetchExpectedChecksumWith(ctx, downloadHTTPClient, version, binaryName)
}

func fetchExpectedChecksumWith(ctx context.Context, client *http.Client, version, binaryName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cosignChecksumsURL(version), nil)
	if err != nil {
		return "", fmt.Errorf("create checksums request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download checksums file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
func verifyFileChecksum(path, expectedHash string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}

	if hex.EncodeToString(hasher.Sum(nil)) != expectedHash {
		return fmt.Errorf("checksum mismatch for %s", path)
	}
	return nil
}

// downloadAndVerify downloads the binary from url, verifies its SHA256 against
// expectedHash, writes it to destPath, and makes it executable.
func downloadAndVerify(ctx context.Context, url, destPath, expectedHash string) error {
	return downloadAndVerifyWith(ctx, downloadHTTPClient, url, destPath, expectedHash)
}

func downloadAndVerifyWith(ctx context.Context, client *http.Client, url, destPath, expectedHash string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download cosign binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

	if _, err := io.Copy(writer, io.LimitReader(resp.Body, maxBinaryDownloadSize)); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download cosign binary: %w", err)
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
