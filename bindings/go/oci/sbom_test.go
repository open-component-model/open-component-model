package oci_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/attestation"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const sbomTestRepository = "ctf.ocm.software/acme/app"

// attestedImage pushes an image for the given platform together with an SPDX
// attestation, in the layout "docker buildx build --sbom=true" produces, and returns
// both index entries.
func attestedImage(t *testing.T, store spec.Store, platform ociImageSpecV1.Platform) (image, attested ociImageSpecV1.Descriptor) {
	t.Helper()
	ctx := t.Context()

	push := func(mediaType string, value any) ociImageSpecV1.Descriptor {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		desc := content.NewDescriptorFromBytes(mediaType, raw)
		require.NoError(t, store.Push(ctx, desc, bytes.NewReader(raw)))
		return desc
	}

	empty := ociImageSpecV1.DescriptorEmptyJSON
	require.NoError(t, store.Push(ctx, empty, bytes.NewReader(empty.Data)))
	config := push(ociImageSpecV1.MediaTypeImageConfig, map[string]any{
		"architecture": platform.Architecture,
		"os":           platform.OS,
		"rootfs":       map[string]any{"type": "layers"},
	})
	image = push(ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{empty},
	})
	image.Platform = &platform

	statement := push(attestation.MediaTypeInTotoStatement, map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": repository.PredicateTypeSPDX,
		"predicate": map[string]any{
			"spdxVersion": "SPDX-2.3",
			"name":        platform.OS + "/" + platform.Architecture,
		},
	})
	statement.Annotations = map[string]string{
		attestation.AnnotationInTotoPredicateType: repository.PredicateTypeSPDX,
	}

	attested = push(ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    empty,
		Layers:    []ociImageSpecV1.Descriptor{statement},
	})
	attested.Annotations = map[string]string{
		attestation.AnnotationDockerReferenceType:   attestation.ReferenceTypeAttestationManifest,
		attestation.AnnotationDockerReferenceDigest: image.Digest.String(),
	}
	attested.Platform = &ociImageSpecV1.Platform{
		Architecture: attestation.PlatformUnknown,
		OS:           attestation.PlatformUnknown,
	}

	return image, attested
}

// multiPlatformImage stores a two platform buildx index and returns its reference.
func multiPlatformImage(t *testing.T) (*ocictf.Store, string) {
	t.Helper()
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	ctfStore := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))

	store, err := ctfStore.StoreForReference(ctx, sbomTestRepository)
	require.NoError(t, err)

	amd64Image, amd64Attestation := attestedImage(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"})
	arm64Image, arm64Attestation := attestedImage(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"})

	raw, err := json.Marshal(ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{amd64Image, amd64Attestation, arm64Image, arm64Attestation},
	})
	require.NoError(t, err)

	index := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, raw)
	require.NoError(t, store.Push(ctx, index, bytes.NewReader(raw)))
	require.NoError(t, store.Tag(ctx, index, "latest"))

	return ctfStore, sbomTestRepository + ":latest"
}

func sbomResource(reference string, extraIdentity runtime.Identity) *descriptor.Resource {
	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta:    descriptor.ObjectMeta{Name: "image", Version: "1.0.0"},
			ExtraIdentity: extraIdentity,
		},
		Type: "ociImage",
	}
	res.Access = &accessv1.OCIImage{
		Type:           runtime.NewVersionedType(accessv1.OCIImageType, "v1"),
		ImageReference: reference,
	}
	return res
}

// documentName reads back the marker the fixture put into each platform's SBOM, so a
// test can tell which platform it was served.
func documentName(t *testing.T, sbom repository.SBOM) string {
	t.Helper()
	var predicate struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(sbom.Data, &predicate))
	return predicate.Name
}

func TestRepository_DiscoverSBOM(t *testing.T) {
	t.Run("uses the platform named by the resource extra identity", func(t *testing.T) {
		ctfStore, reference := multiPlatformImage(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore))

		res := sbomResource(reference, runtime.Identity{"architecture": "arm64", "os": "linux"})

		sboms, err := repo.DiscoverSBOM(t.Context(), res)
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, "linux/arm64", documentName(t, sboms[0]))
	})

	t.Run("an explicit platform option overrides the resource", func(t *testing.T) {
		ctfStore, reference := multiPlatformImage(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore))

		res := sbomResource(reference, runtime.Identity{"architecture": "arm64", "os": "linux"})

		sboms, err := repo.DiscoverSBOM(t.Context(), res,
			repository.WithSBOMPlatform(repository.Platform{OS: "linux", Architecture: "amd64"}))
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, "linux/amd64", documentName(t, sboms[0]))
	})

	t.Run("an explicit platform option applies to a resource naming no platform", func(t *testing.T) {
		ctfStore, reference := multiPlatformImage(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore))

		res := sbomResource(reference, nil)

		sboms, err := repo.DiscoverSBOM(t.Context(), res,
			repository.WithSBOMPlatform(repository.Platform{Architecture: "amd64"}))
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, "linux/amd64", documentName(t, sboms[0]))
	})

	t.Run("an explicit platform option refines the resource attribute by attribute", func(t *testing.T) {
		ctfStore, reference := multiPlatformImage(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore))

		res := sbomResource(reference, runtime.Identity{"os": "linux"})

		sboms, err := repo.DiscoverSBOM(t.Context(), res,
			repository.WithSBOMPlatform(repository.Platform{Architecture: "arm64"}))
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, "linux/arm64", documentName(t, sboms[0]))
	})

	t.Run("reports a platform the image does not offer", func(t *testing.T) {
		ctfStore, reference := multiPlatformImage(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore))

		res := sbomResource(reference, runtime.Identity{"architecture": "s390x", "os": "linux"})

		_, err := repo.DiscoverSBOM(t.Context(), res)
		require.ErrorIs(t, err, attestation.ErrPlatformNotFound)
	})

	t.Run("rejects an access that is not an oci image", func(t *testing.T) {
		ctfStore, _ := multiPlatformImage(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore))

		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "text", Version: "1.0.0"}},
		}
		res.Access = &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1"}`)}

		_, err := repo.DiscoverSBOM(t.Context(), res)
		require.Error(t, err)
	})

	t.Run("rejects an empty access", func(t *testing.T) {
		ctfStore, _ := multiPlatformImage(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore))

		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "text", Version: "1.0.0"}},
		}

		_, err := repo.DiscoverSBOM(t.Context(), res)
		require.ErrorContains(t, err, "access type is empty")
	})
}
