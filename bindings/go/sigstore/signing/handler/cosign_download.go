package handler

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

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
// Security note (TOCTOU): The cache check (Stat) and binary execution are two
// separate non-atomic steps. On a shared machine, a malicious local user with
// write access to the cache directory could replace the verified binary between
// Stat and exec. The cache directory is created with 0o700 permissions (owner-only)
// which eliminates this risk for the common case. On network filesystems or when
// running as root, additional hardening (e.g. executing from an fd rather than
// a path) would be needed.
func ensureOrDownloadCosign() (string, error) {
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
		return cachedPath, nil
	}

	expectedHash, err := fetchExpectedChecksum(CosignVersion, binaryName)
	if err != nil {
		return "", fmt.Errorf("fetch checksums: %w", err)
	}

	binaryURL := cosignDownloadURL(CosignVersion, runtime.GOOS, runtime.GOARCH)
	if err := downloadAndVerify(binaryURL, cachedPath, expectedHash); err != nil {
		return "", err
	}

	return cachedPath, nil
}

// downloadHTTPClient is used for all cosign download HTTP requests.
// It enforces a 2-minute timeout to prevent indefinite hangs inside sync.Once.
var downloadHTTPClient = &http.Client{Timeout: 2 * time.Minute}

// fetchExpectedChecksum downloads the cosign_checksums.txt for the given version
// and returns the SHA256 hash for the specified binary name.
//
// Security note (trust chain): Both the binary and the checksums file are fetched
// from the same GitHub release. This verifies against accidental data corruption
// but does NOT protect against a malicious GitHub release where both files are
// replaced atomically, or against a network-level MitM that can intercept HTTPS
// (e.g. corporate TLS inspection). The current approach is a pragmatic trade-off:
// integrity without release authenticity.
//
// TODO(ocm-project#996): Upgrade to Sigstore bundle verification. Each cosign
// release ships a cosign_checksums.txt.sigstore.json bundle (v0.3+json) containing
// a Fulcio certificate + Rekor inclusion proof. Verify that bundle against the
// checksums content using a minimal hand-rolled verifier (pinned Sigstore root CA,
// stdlib crypto/x509 + crypto/ecdsa) to preserve the zero-sigstore-deps constraint.
// This turns the current integrity-only check into integrity + authenticity.
func fetchExpectedChecksum(version, binaryName string) (string, error) {
	resp, err := downloadHTTPClient.Get(cosignChecksumsURL(version)) //nolint:noctx // URL is constructed from trusted constants, not user input
	if err != nil {
		return "", fmt.Errorf("download checksums file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksums file: HTTP %d", resp.StatusCode)
	}

	return parseChecksum(resp.Body, binaryName)
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

// downloadAndVerify downloads the binary from url, verifies its SHA256 against
// expectedHash, writes it to destPath, and makes it executable.
func downloadAndVerify(url, destPath, expectedHash string) error {
	resp, err := downloadHTTPClient.Get(url) //nolint:noctx // URL is constructed from trusted constants
	if err != nil {
		return fmt.Errorf("download cosign binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download cosign binary: HTTP %d from %s", resp.StatusCode, url)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), "cosign-download-*")
	if err != nil {
		return fmt.Errorf("create temp download file: %w", err)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = tmpFile.Close()
			_ = os.Remove(tmpFile.Name())
		}
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("download cosign binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("flush cosign binary download: %w", err)
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("cosign binary checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	if err := os.Chmod(tmpFile.Name(), 0o700); err != nil {
		return fmt.Errorf("make cosign binary executable: %w", err)
	}

	if err := os.Rename(tmpFile.Name(), destPath); err != nil {
		return fmt.Errorf("move cosign binary to cache: %w", err)
	}
	renamed = true

	return nil
}
