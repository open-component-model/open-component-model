package referrer

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/content/memory"
	"github.com/oras-project/oras-go/v3/errdef"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ociattestation "ocm.software/open-component-model/bindings/go/oci/internal/attestation"
	"ocm.software/open-component-model/bindings/go/repository"
)

// OpenVEX vocabulary, inlined because the unopinionated model defines no type constants.
const (
	mediaTypeOpenVEX     = "application/openvex+json"
	predicateTypeOpenVEX = "https://openvex.dev/ns/v0.2.0"
)

// push stores data under its content descriptor and returns it.
func push(t *testing.T, store *memory.Store, mediaType string, data []byte) ociImageSpecV1.Descriptor {
	t.Helper()
	desc := content.NewDescriptorFromBytes(mediaType, data)
	if err := store.Push(t.Context(), desc, bytes.NewReader(data)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		require.NoError(t, err)
	}
	return desc
}

// pushManifest marshals and stores a referrer manifest, returning its descriptor with
// the artifact type set the way registry.Referrers would report it.
func pushManifest(t *testing.T, store *memory.Store, manifest ociImageSpecV1.Manifest) ociImageSpecV1.Descriptor {
	t.Helper()
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, raw)
	desc.ArtifactType = manifest.ArtifactType
	desc.Annotations = manifest.Annotations
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(raw)))
	return desc
}

func emptyConfig(t *testing.T, store *memory.Store) ociImageSpecV1.Descriptor {
	t.Helper()
	return push(t, store, mediaTypeEmpty, []byte("{}"))
}

// pushSubject stores a minimal valid image manifest to act as the referrer subject. The
// memory store parses manifests on push to index predecessors, so the subject must be a
// well-formed manifest, not an arbitrary blob.
func pushSubject(t *testing.T, store *memory.Store) ociImageSpecV1.Descriptor {
	t.Helper()
	return pushManifest(t, store, ociImageSpecV1.Manifest{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    emptyConfig(t, store),
	})
}

// TestFromReferrer_PlainBlob reads an oras-attached OpenVEX blob from metadata alone, no
// blob fetch, no interpretation.
func TestFromReferrer_PlainBlob(t *testing.T) {
	store := memory.New()
	subject := pushSubject(t, store)

	blob := push(t, store, mediaTypeOpenVEX, []byte(`{"@context":"https://openvex.dev/ns/v0.2.0"}`))
	ref := pushManifest(t, store, ociImageSpecV1.Manifest{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: mediaTypeOpenVEX,
		Config:       emptyConfig(t, store),
		Layers:       []ociImageSpecV1.Descriptor{blob},
		Subject:      &subject,
	})

	got, err := fromReferrer(t.Context(), store, subject, ref)
	require.NoError(t, err)
	assert.Equal(t, repository.AttestationSourceOCIReferrer, got.Source)
	assert.Equal(t, mediaTypeOpenVEX, got.ArtifactType)
	assert.Empty(t, got.PredicateType, "a plain blob carries no in-toto predicate")
	assert.Equal(t, mediaTypeOpenVEX, got.MediaType)
	assert.Equal(t, subject.Digest.String(), got.Digest)
	assert.Equal(t, ref.Digest.String(), got.ID)
}

// TestFromReferrer_SigstoreBundle reads the predicate type from the bundle annotation,
// without unwrapping the DSSE envelope.
func TestFromReferrer_SigstoreBundle(t *testing.T) {
	store := memory.New()
	subject := pushSubject(t, store)

	const bundleMediaType = "application/vnd.dev.sigstore.bundle.v0.3+json"
	layer := push(t, store, bundleMediaType, []byte(`{"dsseEnvelope":{}}`))
	ref := pushManifest(t, store, ociImageSpecV1.Manifest{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: bundleMediaType,
		Annotations:  map[string]string{annotationSigstorePredicateType: predicateTypeOpenVEX},
		Config:       emptyConfig(t, store),
		Layers:       []ociImageSpecV1.Descriptor{layer},
		Subject:      &subject,
	})

	got, err := fromReferrer(t.Context(), store, subject, ref)
	require.NoError(t, err)
	assert.Equal(t, predicateTypeOpenVEX, got.PredicateType)
	assert.Equal(t, bundleMediaType, got.ArtifactType)
	assert.Equal(t, predicateTypeOpenVEX, got.Type(), "the predicate type is the most specific signal")
}

// TestFromReferrer_InToto reads the predicate type from the in-toto layer annotation,
// without fetching the statement.
func TestFromReferrer_InToto(t *testing.T) {
	store := memory.New()
	subject := pushSubject(t, store)

	layer := content.NewDescriptorFromBytes(ociattestation.MediaTypeInTotoStatement, []byte(`{"predicate":{}}`))
	layer.Annotations = map[string]string{ociattestation.AnnotationInTotoPredicateType: repository.PredicateTypeSPDX}
	require.NoError(t, store.Push(t.Context(), layer, bytes.NewReader([]byte(`{"predicate":{}}`))))

	ref := pushManifest(t, store, ociImageSpecV1.Manifest{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    emptyConfig(t, store),
		Layers:    []ociImageSpecV1.Descriptor{layer},
		Subject:   &subject,
	})

	got, err := fromReferrer(t.Context(), store, subject, ref)
	require.NoError(t, err)
	assert.Equal(t, repository.PredicateTypeSPDX, got.PredicateType)
	assert.Equal(t, ociattestation.MediaTypeInTotoStatement, got.MediaType)
	assert.Equal(t, repository.PredicateTypeSPDX, got.Type())
}

// TestDiscoverAttestations_KeepsEverything lists every referrer, signatures included, and
// never drops or interprets them.
func TestDiscoverAttestations_KeepsEverything(t *testing.T) {
	store := memory.New()
	subject := pushSubject(t, store)

	const cosignSig = "application/vnd.dev.cosign.artifact.sig.v1+json"
	sigBlob := push(t, store, "application/vnd.dev.cosign.simplesigning.v1+json", []byte(`sig`))
	pushManifest(t, store, ociImageSpecV1.Manifest{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: cosignSig,
		Config:       emptyConfig(t, store),
		Layers:       []ociImageSpecV1.Descriptor{sigBlob},
		Subject:      &subject,
	})

	got, err := DiscoverAttestations(t.Context(), store, subject)
	require.NoError(t, err)
	require.Len(t, got, 1, "a signature referrer is surfaced, not skipped")
	assert.Equal(t, cosignSig, got[0].ArtifactType)
	assert.Equal(t, cosignSig, got[0].Type())
}
