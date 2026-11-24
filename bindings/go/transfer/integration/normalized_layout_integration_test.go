package integration_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ociresource "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
)

// cosignSignatureArtifactType and cosignAttestationArtifactType are the artifact types cosign uses
// for signatures and attestations. The test only needs non-access referrers that the carry copies
// verbatim; the exact values stand in for real cosign artifacts.
const (
	cosignSignatureArtifactType   = "application/vnd.dev.cosign.artifact.sig.v1+json"
	cosignAttestationArtifactType = "application/vnd.dev.cosign.artifact.sbom.v1+json"
)

// Test_Integration_TransferNormalized_OCIToOCI_CarriesSignatureReferrer proves the central promise
// of the normalized OCI layout (ADR 0034) against two real registries: the tag resolves to a
// stable, access-free normalized manifest whose digest (D_norm) is invariant across a
// registry-to-registry transfer, and a cosign-style signature attached as a referrer of that
// manifest is carried to the target verbatim. Because the signed bytes (the manifest at D_norm)
// and the signature referrer are byte-identical on both registries, a signature made on the source
// stays valid on the target without re-signing.
//
// A real cosign signature is exercised by the conformance scenario; here the signature is a
// referrer manifest pushed directly, so the transfer mechanics can be asserted in Go.
func Test_Integration_TransferNormalized_OCIToOCI_CarriesSignatureReferrer(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// 1. Two independent registries, standing in for "source" and "target".
	sourceAddr, sourceUser, sourcePwd := startRegistry(t)
	targetAddr, targetUser, targetPwd := startRegistry(t)

	const (
		componentName    = "acme.org/normalized/transfer"
		componentVersion = "1.0.0"
		resourceName     = "payload"
	)
	resourceData := []byte("content bound to the signed normalized descriptor")

	// 2. Publish the component version in the normalized layout on the source registry.
	sourceRepo := newLiveOCIRepository(t, sourceAddr, sourceUser, sourcePwd, oci.WithComponentVersionLayout(oci.LayoutNormalized))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "acme"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: componentVersion},
					},
					Type:     "plainText",
					Relation: descriptor.LocalRelation,
					Access: &descriptorv2.LocalBlob{
						Type:      runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
						MediaType: "text/plain",
					},
				},
			},
		},
	}
	updated, err := sourceRepo.AddLocalResource(ctx, componentName, componentVersion,
		&desc.Component.Resources[0], inmemory.New(bytes.NewReader(resourceData)))
	r.NoError(err)
	desc.Component.Resources[0] = *updated
	r.NoError(sourceRepo.AddComponentVersion(ctx, desc))

	// 3. D_norm: the digest the tag resolves to on the source. This is what cosign would sign.
	sourcePin, ok := sourceRepo.(repository.DigestPinnableRepository)
	r.True(ok, "the OCI repository must expose the component version digest")
	dNormSource, err := sourcePin.ResolveComponentVersionDigest(ctx, componentName, componentVersion)
	r.NoError(err)
	r.NotEmpty(dNormSource)

	// 4. Attach cosign-style referrers (a signature and an attestation) to D_norm on the source.
	//    Both are non-access referrers that the carry must copy verbatim.
	sourceRemote := newRemoteRepository(t, sourceAddr, componentName, sourceUser, sourcePwd)
	dNormDesc, err := sourceRemote.Resolve(ctx, dNormSource)
	r.NoError(err)
	var dNormManifest ociImageSpecV1.Manifest
	r.NoError(json.Unmarshal(fetchAll(t, ctx, sourceRemote, dNormDesc), &dNormManifest))
	r.Equal(descriptorArtifactTypeNormalized, dNormManifest.ArtifactType,
		"the tag must resolve to the normalized manifest, not the access-bearing descriptor")

	signatureDesc := pushReferrer(t, ctx, sourceRemote, dNormDesc, cosignSignatureArtifactType, "cosign signature")
	attestationDesc := pushReferrer(t, ctx, sourceRemote, dNormDesc, cosignAttestationArtifactType, "cosign attestation")
	referrerBytes := map[string][]byte{
		signatureDesc.Digest.String():   fetchAll(t, ctx, sourceRemote, signatureDesc),
		attestationDesc.Digest.String(): fetchAll(t, ctx, sourceRemote, attestationDesc),
	}

	// 5. Transfer source -> target without a layout flag: the target inherits the normalized layout
	//    and the signature referrer is carried along.
	sourceSpec := &ocirepospec.Repository{
		Type:                   runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl:                fmt.Sprintf("http://%s", sourceAddr),
		ComponentVersionLayout: layout.ComponentVersionLayoutNormalized,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", targetAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(ctx, nil, nil,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(sourceRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotEmpty(tgd.Transformations)

	credResolver := newCredResolver(t,
		registryCreds{sourceAddr, sourceUser, sourcePwd},
		registryCreds{targetAddr, targetUser, targetPwd},
	)
	repoProvider := provider.NewComponentVersionRepositoryProvider(
		provider.WithTempDir(t.TempDir()), provider.WithHTTPConfig(plainHTTPConfig()),
	)
	resourceRepo := ociresource.NewResourceRepository(nil, ociresource.WithHTTPConfig(plainHTTPConfig()))
	builder := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver, transfer.WithHTTPConfig(plainHTTPConfig()))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx), "transfer graph execution should succeed")

	// 6. D_norm is invariant across the transfer: the signature over it stays valid.
	targetRepo := newLiveOCIRepository(t, targetAddr, targetUser, targetPwd, oci.WithComponentVersionLayout(oci.LayoutNormalized))
	targetPin, ok := targetRepo.(repository.DigestPinnableRepository)
	r.True(ok)
	dNormTarget, err := targetPin.ResolveComponentVersionDigest(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Equal(dNormSource, dNormTarget, "the normalized manifest digest must survive the transfer unchanged")

	// 7. Every cosign referrer is carried verbatim: same digests and identical bytes on the target.
	//    The target still gets its own access referrer, regenerated locally (not carried), which is
	//    what makes the component readable below.
	targetRemote := newRemoteRepository(t, targetAddr, componentName, targetUser, targetPwd)
	dNormDescTarget, err := targetRemote.Resolve(ctx, dNormTarget)
	r.NoError(err)
	carried := collectReferrers(t, ctx, targetRemote, dNormDescTarget)
	carriedDigests := digestsOf(carried)
	for wantDigest, wantBytes := range referrerBytes {
		r.Contains(carriedDigests, wantDigest, "cosign referrer must be present on the target after transfer")
		gotBytes := fetchAll(t, ctx, targetRemote, ociImageSpecV1.Descriptor{
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Digest:    godigest.Digest(wantDigest),
			Size:      int64(len(wantBytes)),
		})
		r.Equal(wantBytes, gotBytes, "the carried referrer manifest must be byte-identical")
	}
	var hasAccessReferrer bool
	for _, ref := range carried {
		if ref.ArtifactType == descriptorArtifactTypeAccess {
			hasAccessReferrer = true
		}
	}
	r.True(hasAccessReferrer, "the target must regenerate its own access referrer so the component is readable")

	// 8. The component and its local content are readable on the target and bind to the signed digests.
	got, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Equal(componentName, got.Component.Name)
	r.Len(got.Component.Resources, 1)

	localBlob, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion,
		runtime.Identity{"name": resourceName})
	r.NoError(err)
	reader, err := localBlob.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()
	gotData, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(resourceData, gotData, "the transferred local content must match the original")
}

const (
	descriptorArtifactTypeNormalized = "application/vnd.ocm.software.component-descriptor.normalized.v1"
	descriptorArtifactTypeAccess     = "application/vnd.ocm.software.component-descriptor.access.v1"
)

// newLiveOCIRepository builds an OCM OCI repository backed by a running registry.
func newLiveOCIRepository(t *testing.T, address, user, password string, opts ...oci.RepositoryOption) repository.ComponentVersionRepository {
	t.Helper()
	res, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(createAuthClient(address, user, password)),
	)
	require.NoError(t, err)
	repo, err := oci.NewRepository(append([]oci.RepositoryOption{
		oci.WithResolver(res), oci.WithTempDir(t.TempDir()),
	}, opts...)...)
	require.NoError(t, err)
	return repo
}

// newRemoteRepository opens a raw oras client on the OCI repository path an OCM component maps to,
// so the test can push and inspect referrers directly.
func newRemoteRepository(t *testing.T, address, component, user, password string) *remote.Repository {
	t.Helper()
	repo, err := remote.NewRepository(fmt.Sprintf("%s/component-descriptors/%s", address, component))
	require.NoError(t, err)
	repo.Registry.PlainHTTP = true
	repo.Registry.Client = createAuthClient(address, user, password)
	return repo
}

// pushReferrer pushes a minimal image manifest of artifactType that refers to subject, standing in
// for a cosign signature or attestation attached to the normalized manifest.
func pushReferrer(t *testing.T, ctx context.Context, repo *remote.Repository, subject ociImageSpecV1.Descriptor, artifactType, payloadPrefix string) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)

	payload := []byte(payloadPrefix + " over " + subject.Digest.String())
	layerDesc := content.NewDescriptorFromBytes("application/octet-stream", payload)
	r.NoError(repo.Push(ctx, layerDesc, bytes.NewReader(payload)))
	r.NoError(repo.Push(ctx, ociImageSpecV1.DescriptorEmptyJSON, bytes.NewReader(ociImageSpecV1.DescriptorEmptyJSON.Data)))

	subjectRef := ociImageSpecV1.Descriptor{MediaType: subject.MediaType, Digest: subject.Digest, Size: subject.Size}
	manifest := ociImageSpecV1.Manifest{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       ociImageSpecV1.DescriptorEmptyJSON,
		Layers:       []ociImageSpecV1.Descriptor{layerDesc},
		Subject:      &subjectRef,
	}
	manifest.SchemaVersion = 2
	raw, err := json.Marshal(manifest, runtime.JSONOptions)
	r.NoError(err)
	manifestDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, raw)
	manifestDesc.ArtifactType = artifactType
	r.NoError(repo.Push(ctx, manifestDesc, bytes.NewReader(raw)))
	return manifestDesc
}

// collectReferrers returns all referrers of subject reported by the registry's referrers API.
func collectReferrers(t *testing.T, ctx context.Context, repo *remote.Repository, subject ociImageSpecV1.Descriptor) []ociImageSpecV1.Descriptor {
	t.Helper()
	var all []ociImageSpecV1.Descriptor
	require.NoError(t, repo.Referrers(ctx, subject, "", func(referrers []ociImageSpecV1.Descriptor) error {
		all = append(all, referrers...)
		return nil
	}))
	return all
}

func digestsOf(descs []ociImageSpecV1.Descriptor) []string {
	out := make([]string, len(descs))
	for i, d := range descs {
		out[i] = d.Digest.String()
	}
	return out
}

func fetchAll(t *testing.T, ctx context.Context, repo *remote.Repository, desc ociImageSpecV1.Descriptor) []byte {
	t.Helper()
	rc, err := repo.Fetch(ctx, desc)
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}
