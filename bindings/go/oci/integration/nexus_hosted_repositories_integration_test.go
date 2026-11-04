package integration_test

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// Test_Integration_OCIRepository_NexusDockerRepository checks whether a Nexus Repository 3
// hosted repository of format docker accepts OCM component versions through its registry v2 API.
func Test_Integration_OCIRepository_NexusDockerRepository(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Nexus docker repository integration test in short mode")
	}
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	resolver, repo, _, _ := startNexusDockerHosted(t, ctx)

	const component, version = "ocm.software/nexus-docker-test", "v1.0.0"
	uploadDownloadBarebonesComponentVersion(t, repo, component, version)

	// Record what Nexus stored: OCM writes an OCI image manifest. If this holds, a
	// Nexus docker repository accepts OCM component versions without format conversion.
	ref := resolver.ComponentVersionReference(ctx, component, version)
	store, err := resolver.StoreForReference(ctx, ref)
	r.NoError(err)
	desc, err := store.Resolve(ctx, ref)
	r.NoError(err)
	t.Logf("nexus stored %s as %s (%s)", ref, desc.MediaType, desc.Digest)
	r.Equal(ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
}
