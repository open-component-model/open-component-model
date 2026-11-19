package internal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCosignDownloadURL(t *testing.T) {
	t.Parallel()
	base := "https://github.com/open-component-model/cosign/releases/download/" + cosignVersion + "/"
	tests := []struct{ name, version, goos, goarch, want string }{
		{"linux amd64", cosignVersion, "linux", "amd64", base + "cosign-linux-amd64"},
		{"darwin arm64", cosignVersion, "darwin", "arm64", base + "cosign-darwin-arm64"},
		{"windows amd64", cosignVersion, "windows", "amd64", base + "cosign-windows-amd64.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, cosignDownloadURL(tt.version, tt.goos, tt.goarch))
		})
	}
}

func TestCosignBinaryName(t *testing.T) {
	t.Parallel()
	tests := []struct{ goos, goarch, want string }{
		{"linux", "amd64", "cosign-linux-amd64"},
		{"darwin", "arm64", "cosign-darwin-arm64"},
		{"windows", "amd64", "cosign-windows-amd64.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, cosignBinaryName(tt.goos, tt.goarch))
		})
	}
}

func TestCosignCachePath(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	path, err := cosignCachePath(cosignVersion)
	r.NoError(err)
	r.Contains(path, "ocm")
	r.Contains(path, "cosign")
	r.Contains(path, cosignVersion)
}

func TestParseChecksum(t *testing.T) {
	t.Parallel()
	content := "abc123def456  cosign-linux-amd64\nfedcba987654  cosign-windows-amd64.exe\n"
	t.Run("finds existing binary", func(t *testing.T) {
		t.Parallel()
		hash, err := parseChecksum(strings.NewReader(content), "cosign-linux-amd64")
		require.NoError(t, err)
		require.Equal(t, "abc123def456", hash)
	})
	t.Run("missing binary returns error", func(t *testing.T) {
		t.Parallel()
		_, err := parseChecksum(strings.NewReader(content), "cosign-freebsd-riscv64")
		require.ErrorContains(t, err, "checksum not found")
	})
}

func TestCosignPins_AreSet(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	r.Regexp(`^v\d+\.\d+\.\d+\+ocm\.\d+$`, cosignVersion, "COSIGN_VERSION must be an OCM cosign release vMAJOR.MINOR.PATCH+ocm.N")
	r.Regexp(`^[0-9a-f]{64}$`, cosignChecksumsSHA256, "COSIGN_CHECKSUMS_SHA256 must be a lowercase hex SHA256")
}

func TestDownloadAndVerify(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho cosign mock\n")
	hash := sha256.Sum256(binaryContent)
	expectedHash := hex.EncodeToString(hash[:])
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(binaryContent)
		}))
		t.Cleanup(srv.Close)
		destPath := filepath.Join(t.TempDir(), "cosign-test")
		r.NoError(downloadAndVerifyWith(ctx, srv.Client(), srv.URL+"/cosign", destPath, expectedHash))
		info, err := os.Stat(destPath)
		r.NoError(err)
		r.True(info.Mode().IsRegular())
		if runtime.GOOS != "windows" { // Windows has no executable permission bit.
			r.NotZero(info.Mode() & 0o100)
		}
	})
	t.Run("checksum mismatch", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("tampered"))
		}))
		t.Cleanup(srv.Close)
		destPath := filepath.Join(t.TempDir(), "cosign-test")
		err := downloadAndVerifyWith(ctx, srv.Client(), srv.URL+"/cosign", destPath, expectedHash)
		require.ErrorContains(t, err, "checksum mismatch")
		_, statErr := os.Stat(destPath)
		require.True(t, os.IsNotExist(statErr))
	})
	t.Run("http error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)
		destPath := filepath.Join(t.TempDir(), "cosign-test")
		require.ErrorContains(t, downloadAndVerifyWith(ctx, srv.Client(), srv.URL+"/cosign", destPath, expectedHash), "HTTP 404")
	})
	t.Run("content-length exceeds safety limit", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "157286401") // maxBinaryDownloadSize + 1
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		destPath := filepath.Join(t.TempDir(), "cosign-test")
		err := downloadAndVerifyWith(ctx, srv.Client(), srv.URL+"/cosign", destPath, "irrelevant")
		require.ErrorContains(t, err, "too large")
	})
}

func TestVerifyFileChecksum(t *testing.T) {
	t.Parallel()
	content := []byte("hello world")
	hash := sha256.Sum256(content)
	validHash := hex.EncodeToString(hash[:])

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "f")
		require.NoError(t, os.WriteFile(path, content, 0o600))
		require.NoError(t, verifyFileChecksum(path, validHash))
	})
	t.Run("mismatch", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "f")
		require.NoError(t, os.WriteFile(path, content, 0o600))
		require.ErrorContains(t, verifyFileChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000"), "checksum mismatch")
	})
}

func TestFetchExpectedChecksumWith(t *testing.T) {
	t.Parallel()
	checksums := []byte("deadbeef12345678  cosign-linux-amd64\n")
	sum := sha256.Sum256(checksums)
	pinned := hex.EncodeToString(sum[:])
	serve := func(t *testing.T, status int, body []byte) *http.Client {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write(body)
		}))
		t.Cleanup(srv.Close)
		return redirectAllClient(srv)
	}
	t.Run("non-200", func(t *testing.T) {
		t.Parallel()
		_, err := fetchExpectedChecksumWith(t.Context(), serve(t, http.StatusForbidden, nil), "cosign-linux-amd64", pinned)
		require.ErrorContains(t, err, "HTTP 403")
	})
	t.Run("checksums file matches pin", func(t *testing.T) {
		t.Parallel()
		hash, err := fetchExpectedChecksumWith(t.Context(), serve(t, http.StatusOK, checksums), "cosign-linux-amd64", pinned)
		require.NoError(t, err)
		require.Equal(t, "deadbeef12345678", hash)
	})
	t.Run("replaced checksums file is rejected", func(t *testing.T) {
		t.Parallel()
		tampered := []byte("0123456789abcdef  cosign-linux-amd64\n")
		_, err := fetchExpectedChecksumWith(t.Context(), serve(t, http.StatusOK, tampered), "cosign-linux-amd64", pinned)
		require.ErrorContains(t, err, "checksums file mismatch")
	})
}

func redirectAllClient(srv *httptest.Server) *http.Client {
	return &http.Client{Transport: &rewriteTransport{base: srv.Client().Transport, serverURL: srv.URL}}
}

type rewriteTransport struct {
	base      http.RoundTripper
	serverURL string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL, _ = req.URL.Parse(rt.serverURL + req.URL.Path)
	return rt.base.RoundTrip(req)
}
