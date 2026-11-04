package internal

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCosignImagePins_MatchDownloadPin keeps the cosign shipped in the CLI and
// controller images on the same OCM cosign build that auto-download fetches. A
// manual bump or a partially merged Renovate PR fails here instead of silently
// running two different cosign builds.
func TestCosignImagePins_MatchDownloadPin(t *testing.T) {
	t.Parallel()
	// OCI tags cannot contain "+", so release v3.1.3+ocm.N is image tag v3.1.3-ocm.N.
	wantTag := strings.Replace(cosignVersion, "+", "-", 1)
	imageRE := regexp.MustCompile(`(?m)^FROM ghcr\.io/open-component-model/cosign:([^@\s]+)@sha256:[0-9a-f]{64} AS cosign$`)

	for _, path := range []string{
		"../../../../cli/Containerfile",
		"../../../../kubernetes/controller/Dockerfile",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			content, err := os.ReadFile(path)
			r.NoError(err)
			matches := imageRE.FindAllStringSubmatch(string(content), -1)
			r.Len(matches, 1, "expected exactly one digest-pinned ghcr.io/open-component-model/cosign stage")
			r.Equal(wantTag, matches[0][1], "image tag must match COSIGN_VERSION in .env")
		})
	}
}
