package handler

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCosignDownloadURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		goos    string
		goarch  string
		want    string
	}{
		{
			name:    "linux amd64",
			version: "v3.0.6",
			goos:    "linux",
			goarch:  "amd64",
			want:    "https://github.com/sigstore/cosign/releases/download/v3.0.6/cosign-linux-amd64",
		},
		{
			name:    "linux arm64",
			version: "v3.0.6",
			goos:    "linux",
			goarch:  "arm64",
			want:    "https://github.com/sigstore/cosign/releases/download/v3.0.6/cosign-linux-arm64",
		},
		{
			name:    "darwin amd64",
			version: "v3.0.6",
			goos:    "darwin",
			goarch:  "amd64",
			want:    "https://github.com/sigstore/cosign/releases/download/v3.0.6/cosign-darwin-amd64",
		},
		{
			name:    "darwin arm64",
			version: "v3.0.6",
			goos:    "darwin",
			goarch:  "arm64",
			want:    "https://github.com/sigstore/cosign/releases/download/v3.0.6/cosign-darwin-arm64",
		},
		{
			name:    "windows amd64",
			version: "v3.0.6",
			goos:    "windows",
			goarch:  "amd64",
			want:    "https://github.com/sigstore/cosign/releases/download/v3.0.6/cosign-windows-amd64.exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			r.Equal(tt.want, cosignDownloadURL(tt.version, tt.goos, tt.goarch))
		})
	}
}

func TestCosignChecksumsURL(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	r.Equal(
		"https://github.com/sigstore/cosign/releases/download/v3.0.6/cosign_checksums.txt",
		cosignChecksumsURL("v3.0.6"),
	)
}

func TestCosignBinaryName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "cosign-linux-amd64"},
		{"darwin", "arm64", "cosign-darwin-arm64"},
		{"windows", "amd64", "cosign-windows-amd64.exe"},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			r.Equal(tt.want, cosignBinaryName(tt.goos, tt.goarch))
		})
	}
}

func TestCosignCachePath(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	path, err := cosignCachePath("v3.0.6")
	r.NoError(err)
	r.Contains(path, "ocm")
	r.Contains(path, "cosign")
	r.Contains(path, "v3.0.6")
}

func TestParseChecksum(t *testing.T) {
	t.Parallel()

	checksumContent := `abc123def456  cosign-linux-amd64
789012345678  cosign-darwin-arm64
fedcba987654  cosign-windows-amd64.exe
`

	t.Run("finds existing binary", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		hash, err := parseChecksum(strings.NewReader(checksumContent), "cosign-linux-amd64")
		r.NoError(err)
		r.Equal("abc123def456", hash)
	})

	t.Run("finds windows binary", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		hash, err := parseChecksum(strings.NewReader(checksumContent), "cosign-windows-amd64.exe")
		r.NoError(err)
		r.Equal("fedcba987654", hash)
	})

	t.Run("missing binary returns error", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		_, err := parseChecksum(strings.NewReader(checksumContent), "cosign-freebsd-riscv64")
		r.Error(err)
		r.Contains(err.Error(), "checksum not found")
	})

	t.Run("empty input returns error", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		_, err := parseChecksum(strings.NewReader(""), "cosign-linux-amd64")
		r.Error(err)
		r.Contains(err.Error(), "checksum not found")
	})
}

func TestCosignVersion_IsSet(t *testing.T) {
	t.Parallel()
	// CosignVersion must be a valid semver tag: vMAJOR.MINOR.PATCH
	// This prevents path-traversal in the constructed download URL and ensures
	// Renovate-managed version bumps stay well-formed.
	semverRE := regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	if !semverRE.MatchString(CosignVersion) {
		t.Fatalf("CosignVersion %q does not match required format vMAJOR.MINOR.PATCH", CosignVersion)
	}
}
