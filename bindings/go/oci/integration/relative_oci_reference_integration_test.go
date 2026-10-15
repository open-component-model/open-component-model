package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_RelativeOCIReference_Read proves the read arm against a real remote OCI
// registry: a component carries a relativeOciReference whose reference is resolved against
// the registry ROOT (not the OCM component-descriptors subtree), fetched over HTTP with the
// resolver's credentials, and materialised as an OCI layout tar — the piece the CTF-backed
// unit tests cannot exercise.
func Test_Integration_RelativeOCIReference_Read(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	r := require.New(t)

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)

	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{"REGISTRY_VALIDATION_DISABLED": "true"}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(registryContainer)) })

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	client := createAuthClient(registryAddress, testUsername, password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	// Stage a single-layer OCI image at the registry-root path ocm/image:v1 — deliberately
	// NOT under the component-descriptors subtree the component itself lives in. A relative
	// reference resolves only if the registry root (and not the OCM subPath) is the base.
	const (
		component   = "acme.org/compo"
		version     = "v1.0.0"
		relativeRef = "ocm/image:v1"
	)
	payload := []byte("relative artifact payload")
	stagedRef := registryAddress + "/" + relativeRef
	layoutBytes, access := createSingleLayerOCIImage(t, payload, stagedRef)
	stageResource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "staged", Version: version}},
		Type:        "ociImage",
		Access:      access,
	}
	_, err = repo.UploadResource(ctx, &stageResource, inmemory.New(bytes.NewReader(layoutBytes)))
	r.NoError(err)

	// A component whose resource points at that artifact via a registry-relative reference.
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "test-provider"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
			Resources: []descriptor.Resource{{
				Relation:    descriptor.LocalRelation,
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "relative-image", Version: version}},
				Type:        "ociImage",
				Access: &v1.RelativeOCIReference{
					Type:      ocmruntime.NewUnversionedType(v1.RelativeOCIReferenceType),
					Reference: relativeRef,
				},
			}},
		},
	}
	r.NoError(repo.AddComponentVersion(ctx, desc))

	// Read it back: a successful fetch proves registry-root resolution + remote credentials.
	blb, res, err := repo.GetLocalResource(ctx, component, version, ocmruntime.Identity{"name": "relative-image"})
	r.NoError(err)
	r.NotNil(blb)

	// The descriptor is not mutated: the returned access is still a relativeOciReference.
	r.True(ocmoci.IsRelativeOCIReference(res.Access),
		"returned access must still be a relativeOciReference, got %T", res.Access)

	// The materialised blob is a valid OCI layout tar whose artifact carries the staged bytes.
	rc, err := blb.ReadCloser()
	r.NoError(err)
	buf, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	store, err := tar.ReadOCILayout(ctx, inmemory.New(bytes.NewReader(buf)))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(store.Close()) })
	r.NotEmpty(store.Index.Manifests)

	mrc, err := store.Fetch(ctx, store.Index.Manifests[0])
	r.NoError(err)
	var manifest ociImageSpecV1.Manifest
	r.NoError(json.NewDecoder(mrc).Decode(&manifest))
	r.NoError(mrc.Close())
	r.Len(manifest.Layers, 1)

	lrc, err := store.Fetch(ctx, manifest.Layers[0])
	r.NoError(err)
	layer, err := io.ReadAll(lrc)
	r.NoError(err)
	r.NoError(lrc.Close())
	r.Equal(payload, layer, "materialised layer must be the staged artifact payload")
}
