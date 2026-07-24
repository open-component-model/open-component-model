package oci_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	intoto "github.com/in-toto/attestation/go/v1"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// fixedStoreResolver is a minimal oci.Resolver that always returns the same
// in-memory store, so FetchImageSBOMs can be exercised without a registry.
type fixedStoreResolver struct {
	store spec.Store
}

func (r fixedStoreResolver) StoreForReference(context.Context, string) (spec.Store, error) {
	return r.store, nil
}

func (r fixedStoreResolver) ComponentVersionReference(_ context.Context, component, version string) string {
	return component + ":" + version
}

func (r fixedStoreResolver) Ping(context.Context) error { return nil }

func pushJSON(t *testing.T, ctx context.Context, store spec.Store, mediaType string, v any) ociImageSpecV1.Descriptor {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	desc := content.NewDescriptorFromBytes(mediaType, data)
	require.NoError(t, store.Push(ctx, desc, bytes.NewReader(data)))
	return desc
}

func pushBlob(t *testing.T, ctx context.Context, store spec.Store, mediaType string, annotations map[string]string, data []byte) ociImageSpecV1.Descriptor {
	t.Helper()
	desc := content.NewDescriptorFromBytes(mediaType, data)
	desc.Annotations = annotations
	require.NoError(t, store.Push(ctx, desc, bytes.NewReader(data)))
	return desc
}

// buildAttestationImage builds an image index containing a real image manifest
// plus a buildx-style attestation manifest whose layers carry an SPDX in-toto
// SBOM and a SLSA provenance statement. It returns the tagged reference and the
// SPDX predicate document that was embedded (for assertion by the caller).
func buildAttestationImage(t *testing.T, ctx context.Context, store spec.Store) (string, []byte) {
	t.Helper()

	// A minimal config + image manifest to stand in for the real image.
	configDesc := pushBlob(t, ctx, store, ociImageSpecV1.MediaTypeImageConfig, nil, []byte(`{}`))
	imageLayer := pushBlob(t, ctx, store, ociImageSpecV1.MediaTypeImageLayerGzip, nil, []byte("layer"))
	imageManifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ociImageSpecV1.Descriptor{imageLayer},
	}
	imageManifestDesc := pushJSON(t, ctx, store, ociImageSpecV1.MediaTypeImageManifest, imageManifest)

	// The in-toto subject references the image manifest by digest.
	subjectDigest := imageManifestDesc.Digest
	algo := subjectDigest.Algorithm().String()
	hex := subjectDigest.Encoded()

	spdxPredicate := []byte(`{"spdxVersion":"SPDX-2.3","name":"sbom"}`)
	spdxStatement := inTotoStatement(t, oci.PredicateTypeSPDX, "podinfo", algo, hex, spdxPredicate)
	provStatement := inTotoStatement(t, "https://slsa.dev/provenance/v1", "podinfo", algo, hex, []byte(`{"buildType":"test"}`))

	// The attestation manifest: an SPDX SBOM layer + a SLSA provenance layer.
	sbomLayer := pushBlob(t, ctx, store, oci.MediaTypeInToto,
		map[string]string{oci.AnnotationInTotoPredicateType: oci.PredicateTypeSPDX}, spdxStatement)
	provLayer := pushBlob(t, ctx, store, oci.MediaTypeInToto,
		map[string]string{oci.AnnotationInTotoPredicateType: "https://slsa.dev/provenance/v1"}, provStatement)
	attManifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    pushBlob(t, ctx, store, ociImageSpecV1.MediaTypeImageConfig, nil, []byte(`{"attestation":true}`)),
		Layers:    []ociImageSpecV1.Descriptor{sbomLayer, provLayer},
	}
	attManifestDesc := pushJSON(t, ctx, store, ociImageSpecV1.MediaTypeImageManifest, attManifest)
	attManifestDesc.Annotations = map[string]string{
		oci.AnnotationDockerReferenceType:   oci.DockerReferenceTypeAttestationManifest,
		oci.AnnotationDockerReferenceDigest: imageManifestDesc.Digest.String(),
	}

	index := ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{imageManifestDesc, attManifestDesc},
	}
	indexDesc := pushJSON(t, ctx, store, ociImageSpecV1.MediaTypeImageIndex, index)
	require.NoError(t, store.Tag(ctx, indexDesc, "6.9.1"))

	return "registry.test/podinfo:6.9.1", spdxPredicate
}

// inTotoStatement builds a valid in-toto v1 Statement (as protojson bytes)
// wrapping the given predicate, with a single subject referencing algo:hex.
func inTotoStatement(t *testing.T, predicateType, subjectName, algo, hex string, predicate []byte) []byte {
	t.Helper()
	pred := &structpb.Struct{}
	require.NoError(t, protojson.Unmarshal(predicate, pred))
	stmt := &intoto.Statement{
		Type:          "https://in-toto.io/Statement/v1",
		PredicateType: predicateType,
		Subject: []*intoto.ResourceDescriptor{
			{Name: subjectName, Digest: map[string]string{algo: hex}},
		},
		Predicate: pred,
	}
	require.NoError(t, stmt.Validate())
	data, err := protojson.Marshal(stmt)
	require.NoError(t, err)
	return data
}

func TestRepository_FetchImageSBOMs_Attestation(t *testing.T) {
	ctx := t.Context()
	store := memory.New()

	ref, spdxPredicate := buildAttestationImage(t, ctx, store)

	repo, err := oci.NewRepository(oci.WithResolver(fixedStoreResolver{store: store}), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)

	sboms, err := repo.FetchImageSBOMs(ctx, ref)
	require.NoError(t, err)
	require.Len(t, sboms, 1, "should find exactly the SPDX SBOM layer, not the provenance layer")

	got := sboms[0]
	require.Equal(t, oci.MediaTypeSPDXJSON, got.MediaType, "blob media type should be the unwrapped SBOM type, not in-toto")
	require.Equal(t, oci.PredicateTypeSPDX, got.PredicateType)
	require.Equal(t, "spdx", got.Format)
	require.Len(t, got.Subjects, 1, "in-toto subjects should be surfaced")
	require.Equal(t, "podinfo", got.Subjects[0].Name)

	rc, err := got.Blob.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	// The blob is the bare, unwrapped predicate — assert on its content rather
	// than byte-equality, since protojson re-marshals field order/spacing.
	require.JSONEq(t, string(spdxPredicate), string(data))
}

func TestRepository_FetchImageSBOMs_NoSBOM(t *testing.T) {
	ctx := t.Context()
	store := memory.New()

	// A plain single-platform image manifest with no attestation manifest and no
	// referrers support (memory.Store is not a ReferrerLister).
	configDesc := pushBlob(t, ctx, store, ociImageSpecV1.MediaTypeImageConfig, nil, []byte(`{}`))
	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ociImageSpecV1.Descriptor{pushBlob(t, ctx, store, ociImageSpecV1.MediaTypeImageLayerGzip, nil, []byte("layer"))},
	}
	manifestDesc := pushJSON(t, ctx, store, ociImageSpecV1.MediaTypeImageManifest, manifest)
	require.NoError(t, store.Tag(ctx, manifestDesc, "1.0.0"))

	repo, err := oci.NewRepository(oci.WithResolver(fixedStoreResolver{store: store}), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)

	sboms, err := repo.FetchImageSBOMs(ctx, "registry.test/plain:1.0.0")
	require.NoError(t, err)
	require.Empty(t, sboms)
}
