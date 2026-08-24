package attestation_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	goruntime "runtime"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci/attestation"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/repository"
)

const testRepository = "ctf.ocm.software/acme/app"

// slsaProvenance is the predicate type buildx puts on the provenance layer that sits
// alongside the SBOM layers in the same attestation manifest.
const slsaProvenance = "https://slsa.dev/provenance/v1"

func newStore(t *testing.T) spec.Store {
	t.Helper()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	store, err := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs)).StoreForReference(t.Context(), testRepository)
	require.NoError(t, err)
	return store
}

func pushBlob(t *testing.T, store spec.Store, mediaType string, raw []byte) ociImageSpecV1.Descriptor {
	t.Helper()
	desc := content.NewDescriptorFromBytes(mediaType, raw)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(raw)))
	return desc
}

func pushJSON(t *testing.T, store spec.Store, mediaType string, value any) ociImageSpecV1.Descriptor {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return pushBlob(t, store, mediaType, raw)
}

// image pushes a platform image manifest and returns its index entry.
func image(t *testing.T, store spec.Store, platform ociImageSpecV1.Platform, manifestMediaType string) ociImageSpecV1.Descriptor {
	t.Helper()
	layer := pushBlob(t, store, ociImageSpecV1.MediaTypeImageLayerGzip,
		[]byte(fmt.Sprintf("payload for %s/%s", platform.OS, platform.Architecture)))
	config := pushJSON(t, store, ociImageSpecV1.MediaTypeImageConfig, map[string]any{
		"architecture": platform.Architecture,
		"os":           platform.OS,
		"rootfs":       map[string]any{"type": "layers"},
	})
	desc := pushJSON(t, store, manifestMediaType, ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: manifestMediaType,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{layer},
	})
	desc.Platform = &platform
	return desc
}

// predicateFor names its documents the way BuildKit does: "sbom" for the image itself
// and "sbom-<stage>" for each scanned build stage. Discovery has no opinion on those
// names, so they are literals here rather than a constant the binding exports.
func predicateFor(index int) map[string]any {
	name := "sbom"
	if index > 0 {
		name = fmt.Sprintf("sbom-stage%d", index)
	}
	return map[string]any{
		"spdxVersion": "SPDX-2.3",
		"SPDXID":      fmt.Sprintf("SPDXRef-DOCUMENT-%d", index),
		"name":        name,
	}
}

// replicate a buildkit attestation
//
//	{
//	   "_type": "https://in-toto.io/Statement/v1",
//	   "predicateType": "https://spdx.dev/Document",
//	   "subject": [],
//	   "predicate": {
//	       "SPDXID": "SPDXRef-DOCUMENT",
//	       "creationInfo": {
//	           "created": "2026-08-12T09:56:35Z",
//	           "creators": [
//	               "Organization: Anchore, Inc",
//	               "Tool: syft-v1.42.3"
//	           ],
//	           "licenseListVersion": "3.28"
//	       },
//	 	...
//	 }
func attestationFor(t *testing.T, store spec.Store, subject ociImageSpecV1.Descriptor, predicateTypes ...string) ociImageSpecV1.Descriptor {
	t.Helper()
	config := pushJSON(t, store, ociImageSpecV1.MediaTypeImageConfig, map[string]any{
		"architecture": attestation.PlatformUnknown,
		"os":           attestation.PlatformUnknown,
		"config":       map[string]any{},
		"rootfs":       map[string]any{"type": "layers", "diff_ids": []string{}},
	})

	layers := make([]ociImageSpecV1.Descriptor, 0, len(predicateTypes))
	for i, predicateType := range predicateTypes {
		statement, err := json.Marshal(map[string]any{
			"_type":         "https://in-toto.io/Statement/v0.1",
			"predicateType": predicateType,
			"subject": []map[string]any{{
				"name":   "_",
				"digest": map[string]string{"sha256": subject.Digest.Encoded()},
			}},
			"predicate": predicateFor(i),
		})
		require.NoError(t, err)

		layer := pushBlob(t, store, attestation.MediaTypeInTotoStatement, statement)
		layer.Annotations = map[string]string{attestation.AnnotationInTotoPredicateType: predicateType}
		layers = append(layers, layer)
	}

	desc := pushJSON(t, store, ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    config,
		Layers:    layers,
	})
	// markers are on the index entry. see above.
	desc.Platform = &ociImageSpecV1.Platform{
		Architecture: attestation.PlatformUnknown,
		OS:           attestation.PlatformUnknown,
	}
	desc.Annotations = map[string]string{
		attestation.AnnotationDockerReferenceType:   attestation.ReferenceTypeAttestationManifest,
		attestation.AnnotationDockerReferenceDigest: subject.Digest.String(),
	}
	return desc
}

func tagIndex(t *testing.T, store spec.Store, mediaType, tag string, entries ...ociImageSpecV1.Descriptor) string {
	t.Helper()
	desc := pushJSON(t, store, mediaType, ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: mediaType,
		Manifests: entries,
	})
	require.NoError(t, store.Tag(t.Context(), desc, tag))
	return testRepository + ":" + tag
}

func buildxIndex(t *testing.T, store spec.Store, predicateTypes ...string) (ref string) {
	t.Helper()
	if len(predicateTypes) == 0 {
		predicateTypes = []string{repository.PredicateTypeSPDX}
	}
	img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
	att := attestationFor(t, store, img, predicateTypes...)
	ref = tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest", img, att)
	return ref
}

func linuxARM64() repository.SBOMOption {
	return repository.WithSBOMPlatform(ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"})
}

func TestFixtureMatchesRealBuildKitOutput(t *testing.T) {
	store := newStore(t)
	img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
	att := attestationFor(t, store, img,
		repository.PredicateTypeSPDX, repository.PredicateTypeSPDX, slsaProvenance)

	raw, err := content.FetchAll(t.Context(), store, att)
	require.NoError(t, err)

	var manifest ociImageSpecV1.Manifest
	require.NoError(t, json.Unmarshal(raw, &manifest))

	assert.Equal(t, 2, manifest.SchemaVersion)
	assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, manifest.MediaType)
	assert.Equal(t, ociImageSpecV1.MediaTypeImageConfig, manifest.Config.MediaType)

	require.Len(t, manifest.Layers, 3)
	for _, layer := range manifest.Layers {
		assert.Equal(t, attestation.MediaTypeInTotoStatement, layer.MediaType)
		assert.NotEmpty(t, layer.Annotations[attestation.AnnotationInTotoPredicateType])
	}
	assert.Equal(t, repository.PredicateTypeSPDX, manifest.Layers[0].Annotations[attestation.AnnotationInTotoPredicateType])
	assert.Equal(t, repository.PredicateTypeSPDX, manifest.Layers[1].Annotations[attestation.AnnotationInTotoPredicateType])
	assert.Equal(t, slsaProvenance, manifest.Layers[2].Annotations[attestation.AnnotationInTotoPredicateType])

	assert.Equal(t, attestation.ReferenceTypeAttestationManifest, att.Annotations[attestation.AnnotationDockerReferenceType])
	assert.Equal(t, img.Digest.String(), att.Annotations[attestation.AnnotationDockerReferenceDigest])
	require.NotNil(t, att.Platform)
	assert.Equal(t, attestation.PlatformUnknown, att.Platform.OS)
	assert.Equal(t, attestation.PlatformUnknown, att.Platform.Architecture)
}

func TestDiscoverSBOMs_RealBuildKitLayerMix(t *testing.T) {
	store := newStore(t)
	ref := buildxIndex(t, store,
		repository.PredicateTypeSPDX, repository.PredicateTypeSPDX, slsaProvenance)

	sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
	require.NoError(t, err)
	require.Len(t, sboms, 2, "both SPDX layers are SBOMs; only the provenance is dropped")
	for _, sbom := range sboms {
		assert.Equal(t, repository.PredicateTypeSPDX, sbom.PredicateType)
	}
	names := []string{sboms[0].Name, sboms[1].Name}
	assert.ElementsMatch(t, []string{"sbom", "sbom-stage1"}, names)
}

func TestDiscoverSBOMs(t *testing.T) {
	t.Run("finds the sbom attached to the requested platform", func(t *testing.T) {
		store := newStore(t)
		ref := buildxIndex(t, store)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.NoError(t, err)
		require.Len(t, sboms, 1)

		got := sboms[0]
		assert.Equal(t, repository.PredicateTypeSPDX, got.PredicateType)
		assert.Equal(t, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, got.Platform)
		var predicate map[string]any
		require.NoError(t, json.Unmarshal(got.Data, &predicate))
		assert.Equal(t, "SPDX-2.3", predicate["spdxVersion"])
		assert.NotContains(t, predicate, "predicateType")
	})

	t.Run("skips provenance layers", func(t *testing.T) {
		store := newStore(t)
		ref := buildxIndex(t, store, slsaProvenance, repository.PredicateTypeSPDX)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, repository.PredicateTypeSPDX, sboms[0].PredicateType)
	})

	t.Run("honours a requested predicate type", func(t *testing.T) {
		store := newStore(t)
		ref := buildxIndex(t, store, repository.PredicateTypeCycloneDX)

		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.ErrorIs(t, err, attestation.ErrNoAttestation, "cyclonedx is not discovered by default")

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64(),
			repository.WithSBOMPredicateTypes(repository.PredicateTypeCycloneDX))
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, repository.PredicateTypeCycloneDX, sboms[0].PredicateType)
	})

	t.Run("defaults to the architecture of the running process but not its os", func(t *testing.T) {
		store := newStore(t)
		host := ociImageSpecV1.Platform{OS: "someotheros", Architecture: goruntime.GOARCH}
		other := ociImageSpecV1.Platform{OS: "someotheros", Architecture: "someotherarch"}
		hostImg := image(t, store, host, ociImageSpecV1.MediaTypeImageManifest)
		otherImg := image(t, store, other, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			otherImg, hostImg,
			attestationFor(t, store, otherImg, repository.PredicateTypeSPDX),
			attestationFor(t, store, hostImg, repository.PredicateTypeSPDX),
		)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref)
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, host, sboms[0].Platform)
	})

	t.Run("layers platform requests attribute by attribute", func(t *testing.T) {
		store := newStore(t)
		amd64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			amd64, arm64,
			attestationFor(t, store, amd64, repository.PredicateTypeSPDX),
			attestationFor(t, store, arm64, repository.PredicateTypeSPDX),
		)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref,
			repository.WithSBOMPlatform(ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}),
			repository.WithSBOMPlatform(ociImageSpecV1.Platform{Architecture: "arm64"}))
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, arm64.Digest, sboms[0].Digest)
		assert.Equal(t, "linux", sboms[0].Platform.OS)
	})

	t.Run("selects the attestation belonging to the requested platform", func(t *testing.T) {
		store := newStore(t)
		amd64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			amd64, arm64,
			attestationFor(t, store, amd64, repository.PredicateTypeSPDX),
			attestationFor(t, store, arm64, repository.PredicateTypeSPDX),
		)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, arm64.Digest, sboms[0].Digest)
	})

	t.Run("constrains the operating system version and features", func(t *testing.T) {
		store := newStore(t)
		windows := ociImageSpecV1.Platform{
			OS: "windows", Architecture: "amd64",
			OSVersion:  "10.0.20348.3692",
			OSFeatures: []string{"win32k"},
		}
		img := image(t, store, windows, ociImageSpecV1.MediaTypeImageManifest)
		att := attestationFor(t, store, img, repository.PredicateTypeSPDX)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest", img, att)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithSBOMPlatform(windows))
		require.NoError(t, err)
		require.Len(t, sboms, 1)

		other := windows
		other.OSVersion = "10.0.17763.7314"
		_, err = attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithSBOMPlatform(other))
		require.ErrorIs(t, err, attestation.ErrPlatformNotFound)

		other = windows
		other.OSFeatures = []string{"hyperv"}
		_, err = attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithSBOMPlatform(other))
		require.ErrorIs(t, err, attestation.ErrPlatformNotFound)
	})

	t.Run("matches operating system features regardless of their order", func(t *testing.T) {
		store := newStore(t)
		windows := ociImageSpecV1.Platform{
			OS: "windows", Architecture: "amd64",
			OSFeatures: []string{"win32k", "hyperv"},
		}
		img := image(t, store, windows, ociImageSpecV1.MediaTypeImageManifest)
		att := attestationFor(t, store, img, repository.PredicateTypeSPDX)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest", img, att)

		reordered := windows
		reordered.OSFeatures = []string{"hyperv", "win32k"}
		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithSBOMPlatform(reordered))
		require.NoError(t, err)
		require.Len(t, sboms, 1)
		assert.Equal(t, []string{"hyperv", "win32k"}, reordered.OSFeatures, "matching must not reorder the requested features")

		subset := windows
		subset.OSFeatures = []string{"hyperv"}
		_, err = attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithSBOMPlatform(subset))
		require.ErrorIs(t, err, attestation.ErrPlatformNotFound)

		superset := windows
		superset.OSFeatures = []string{"hyperv", "win32k", "nested"}
		_, err = attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithSBOMPlatform(superset))
		require.ErrorIs(t, err, attestation.ErrPlatformNotFound)
	})

	t.Run("resolves a digest pinned reference", func(t *testing.T) {
		store := newStore(t)
		img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		att := attestationFor(t, store, img, repository.PredicateTypeSPDX)
		index := pushJSON(t, store, ociImageSpecV1.MediaTypeImageIndex, ociImageSpecV1.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ociImageSpecV1.MediaTypeImageIndex,
			Manifests: []ociImageSpecV1.Descriptor{img, att},
		})

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store,
			testRepository+"@"+index.Digest.String(), linuxARM64())
		require.NoError(t, err)
		require.Len(t, sboms, 1)
	})

	t.Run("handles docker manifest list media types", func(t *testing.T) {
		store := newStore(t)
		img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, introspection.MediaTypeDockerManifest)
		att := attestationFor(t, store, img, repository.PredicateTypeSPDX)
		ref := tagIndex(t, store, introspection.MediaTypeDockerManifestList, "latest", img, att)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.NoError(t, err)
		require.Len(t, sboms, 1)
	})
}

func TestDiscoverSBOMs_AllPlatforms(t *testing.T) {
	t.Run("returns the sboms of every platform in the index", func(t *testing.T) {
		store := newStore(t)
		amd64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			amd64, arm64,
			attestationFor(t, store, amd64, repository.PredicateTypeSPDX, repository.PredicateTypeSPDX),
			attestationFor(t, store, arm64, repository.PredicateTypeSPDX),
		)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64(), repository.WithAllSBOMPlatforms())
		require.NoError(t, err)
		require.Len(t, sboms, 3, "both of amd64's documents and arm64's single one")

		byPlatform := map[string]int{}
		for _, sbom := range sboms {
			byPlatform[sbom.Platform.OS+"/"+sbom.Platform.Architecture]++
		}
		assert.Equal(t, map[string]int{"linux/amd64": 2, "linux/arm64": 1}, byPlatform)
	})

	t.Run("skips a platform carrying no attestation instead of failing", func(t *testing.T) {
		store := newStore(t)
		amd64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			amd64, arm64,
			attestationFor(t, store, arm64, repository.PredicateTypeSPDX),
		)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithAllSBOMPlatforms())
		require.NoError(t, err, "one unattested platform must not sink the whole run")
		require.Len(t, sboms, 1)
		assert.Equal(t, arm64.Digest, sboms[0].Digest)
	})

	t.Run("reports when no platform in the index is attested", func(t *testing.T) {
		store := newStore(t)
		amd64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest", amd64, arm64)

		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithAllSBOMPlatforms())
		require.ErrorIs(t, err, attestation.ErrNoAttestation)
	})

	t.Run("reports when every platform is attested but none matches the predicate type", func(t *testing.T) {
		store := newStore(t)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			arm64, attestationFor(t, store, arm64, slsaProvenance),
		)

		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithAllSBOMPlatforms())
		require.ErrorIs(t, err, attestation.ErrNoAttestation)
		assert.Contains(t, err.Error(), repository.PredicateTypeSPDX)
	})

	t.Run("still ignores the unknown/unknown attestation placeholder", func(t *testing.T) {
		store := newStore(t)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			arm64, attestationFor(t, store, arm64, repository.PredicateTypeSPDX),
		)

		sboms, err := attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithAllSBOMPlatforms())
		require.NoError(t, err)
		require.Len(t, sboms, 1, "the attestation manifest is not itself an image to inspect")
		assert.NotEqual(t, attestation.PlatformUnknown, sboms[0].Platform.OS)
	})
}

func TestDiscoverSBOMs_Errors(t *testing.T) {
	t.Run("rejects a plain image manifest", func(t *testing.T) {
		store := newStore(t)
		img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		require.NoError(t, store.Tag(t.Context(), img, "latest"))

		_, err := attestation.DiscoverSBOMs(t.Context(), store, testRepository+":latest", linuxARM64())
		require.ErrorIs(t, err, attestation.ErrNotAnIndex)
	})

	t.Run("reports a requested platform that is absent", func(t *testing.T) {
		store := newStore(t)
		amd64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			amd64, attestationFor(t, store, amd64, repository.PredicateTypeSPDX))

		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.ErrorIs(t, err, attestation.ErrPlatformNotFound)
	})

	t.Run("reports a platform that carries no attestation", func(t *testing.T) {
		store := newStore(t)
		amd64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "amd64"}, ociImageSpecV1.MediaTypeImageManifest)
		arm64 := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest",
			amd64, arm64, attestationFor(t, store, amd64, repository.PredicateTypeSPDX))

		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.ErrorIs(t, err, attestation.ErrNoAttestation)
	})

	t.Run("never matches the attestation manifest itself", func(t *testing.T) {
		store := newStore(t)
		ref := buildxIndex(t, store)
		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, repository.WithSBOMPlatform(
			ociImageSpecV1.Platform{OS: attestation.PlatformUnknown, Architecture: attestation.PlatformUnknown}))
		require.ErrorIs(t, err, attestation.ErrPlatformNotFound)
	})

	t.Run("rejects a statement without a predicate", func(t *testing.T) {
		store := newStore(t)
		img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)

		empty := ociImageSpecV1.DescriptorEmptyJSON
		require.NoError(t, store.Push(t.Context(), empty, bytes.NewReader(empty.Data)))
		layer := pushJSON(t, store, attestation.MediaTypeInTotoStatement, map[string]any{
			"_type":         "https://in-toto.io/Statement/v0.1",
			"predicateType": repository.PredicateTypeSPDX,
		})
		layer.Annotations = map[string]string{attestation.AnnotationInTotoPredicateType: repository.PredicateTypeSPDX}
		att := pushJSON(t, store, ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Config:    empty,
			Layers:    []ociImageSpecV1.Descriptor{layer},
		})
		att.Annotations = map[string]string{
			attestation.AnnotationDockerReferenceType:   attestation.ReferenceTypeAttestationManifest,
			attestation.AnnotationDockerReferenceDigest: img.Digest.String(),
		}
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest", img, att)

		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		// A layer we were looking for and cannot read is reported, not skipped, and
		// the message has to say the sbom is broken rather than missing.
		require.ErrorContains(t, err, "malformed sbom")
		require.ErrorContains(t, err, "carries no predicate document")
		require.NotErrorIs(t, err, attestation.ErrNoAttestation, "a broken sbom is not an absent one")
	})

	t.Run("skips an in-toto layer with no predicate type annotation", func(t *testing.T) {
		store := newStore(t)
		img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)

		empty := ociImageSpecV1.DescriptorEmptyJSON
		require.NoError(t, store.Push(t.Context(), empty, bytes.NewReader(empty.Data)))
		layer := pushJSON(t, store, attestation.MediaTypeInTotoStatement, map[string]any{"predicate": map[string]any{}})
		att := pushJSON(t, store, ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Config:    empty,
			Layers:    []ociImageSpecV1.Descriptor{layer},
		})
		att.Annotations = map[string]string{
			attestation.AnnotationDockerReferenceType:   attestation.ReferenceTypeAttestationManifest,
			attestation.AnnotationDockerReferenceDigest: img.Digest.String(),
		}
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest", img, att)
		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.ErrorIs(t, err, attestation.ErrNoAttestation)
	})

	t.Run("ignores an attestation with no subject annotation", func(t *testing.T) {
		store := newStore(t)
		img := image(t, store, ociImageSpecV1.Platform{OS: "linux", Architecture: "arm64"}, ociImageSpecV1.MediaTypeImageManifest)
		att := attestationFor(t, store, img, repository.PredicateTypeSPDX)
		delete(att.Annotations, attestation.AnnotationDockerReferenceDigest)
		ref := tagIndex(t, store, ociImageSpecV1.MediaTypeImageIndex, "latest", img, att)

		_, err := attestation.DiscoverSBOMs(t.Context(), store, ref, linuxARM64())
		require.ErrorIs(t, err, attestation.ErrNoAttestation)
	})
}
