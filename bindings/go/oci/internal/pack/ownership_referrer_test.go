package pack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func fetchManifest(t *testing.T, ctx context.Context, store *memory.Store, desc ociImageSpecV1.Descriptor) ociImageSpecV1.Manifest {
	t.Helper()
	raw, err := content.FetchAll(ctx, store, desc)
	require.NoError(t, err)
	var m ociImageSpecV1.Manifest
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

// materializeOwnershipReferrer calls OwnershipReferrer and pushes every
// returned referrer's raw bytes directly into dst, mirroring what the
// OCI-layout copy proxy does during artifact upload.
func materializeOwnershipReferrer(t *testing.T, ctx context.Context, dst *memory.Store, subject ociImageSpecV1.Descriptor, artifact descriptor.Artifact, component, version string) error {
	t.Helper()
	referrers, err := OwnershipReferrer(artifact, component, version)(ctx, subject)
	if err != nil {
		return err
	}
	for _, r := range referrers {
		if pushErr := dst.Push(ctx, r.Descriptor, bytes.NewReader(r.Raw)); pushErr != nil {
			if !errors.Is(pushErr, errdef.ErrAlreadyExists) {
				return pushErr
			}
		}
	}
	return nil
}

func TestPushOwnershipReferrer(t *testing.T) {
	t.Run("manifest subject pushes one referrer with correct fields", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := memory.New()

		subject := ociImageSpecV1.Descriptor{
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Digest:    digest.FromBytes([]byte("subject")),
			Size:      7,
		}
		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta:    descriptor.ObjectMeta{Name: "my-resource", Version: "1.0.0"},
				ExtraIdentity: map[string]string{"architecture": "amd64", "os": "linux"},
			},
		}

		r.NoError(materializeOwnershipReferrer(t, ctx, store, subject, resource, "ocm.software/my-component", "1.0.0"))

		// memory.Store indexes Subject as a successor on Push, so the
		// referrer is discoverable as a predecessor of the subject.
		predecessors, err := store.Predecessors(ctx, subject)
		r.NoError(err)
		r.Len(predecessors, 1, "exactly one ownership referrer must be written per call")
		m := fetchManifest(t, ctx, store, predecessors[0])

		t.Run("artifactType matches constant", func(t *testing.T) {
			assert.Equal(t, annotations.OwnershipArtifactType, m.ArtifactType)
		})

		t.Run("subject matches input descriptor", func(t *testing.T) {
			r := require.New(t)
			r.NotNil(m.Subject)
			assert.Equal(t, subject.Digest, m.Subject.Digest)
			assert.Equal(t, subject.MediaType, m.Subject.MediaType)
			assert.Equal(t, subject.Size, m.Subject.Size)
		})

		t.Run("annotations carry component identity and full artifact identity with kind", func(t *testing.T) {
			assert.Equal(t, "ocm.software/my-component", m.Annotations[annotations.OwnershipComponentName])
			assert.Equal(t, "1.0.0", m.Annotations[annotations.OwnershipComponentVersion])
			// Byte-equal (not JSONEq) so a regression that breaks JCS-canonical
			// key ordering on the extraIdentity-laden ToIdentity() output is caught.
			assert.Equal(t,
				`{"identity":{"architecture":"amd64","name":"my-resource","os":"linux","version":"1.0.0"},"kind":"resource"}`,
				m.Annotations[annotations.ArtifactAnnotationKey],
			)
		})

		t.Run("uses empty config and single empty layer per OCI 1.1 guidance", func(t *testing.T) {
			r := require.New(t)
			assert.Equal(t, ociImageSpecV1.MediaTypeEmptyJSON, m.Config.MediaType)
			r.Len(m.Layers, 1)
			assert.Equal(t, ociImageSpecV1.MediaTypeEmptyJSON, m.Layers[0].MediaType)
		})

		t.Run("omits org.opencontainers.image.created so the manifest is content-addressed", func(t *testing.T) {
			_, hasCreated := m.Annotations[ociImageSpecV1.AnnotationCreated]
			assert.Falsef(t, hasCreated,
				"ownership referrer must not carry %s; its presence would make the manifest digest non-deterministic across re-runs",
				ociImageSpecV1.AnnotationCreated)
		})
	})

	t.Run("repeated pushes with identical inputs are idempotent", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := memory.New()

		subject := ociImageSpecV1.Descriptor{
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Digest:    digest.FromBytes([]byte("subject")),
			Size:      7,
		}
		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta:    descriptor.ObjectMeta{Name: "my-resource", Version: "1.0.0"},
				ExtraIdentity: map[string]string{"architecture": "amd64", "os": "linux"},
			},
		}

		// Three back-to-back pushes simulate re-runs of `ocm add cv` against the
		// same target. Without org.opencontainers.image.created the manifest is
		// fully deterministic, so all attempts must yield the same digest and
		// only one referrer must be visible.
		r.NoError(materializeOwnershipReferrer(t, ctx, store, subject, resource, "ocm.software/c", "1.0.0"))
		r.NoError(materializeOwnershipReferrer(t, ctx, store, subject, resource, "ocm.software/c", "1.0.0"))
		r.NoError(materializeOwnershipReferrer(t, ctx, store, subject, resource, "ocm.software/c", "1.0.0"))

		predecessors, err := store.Predecessors(ctx, subject)
		r.NoError(err)
		assert.Lenf(t, predecessors, 1,
			"identical pushes must collapse to a single referrer; got %d distinct manifest digests", len(predecessors))
	})

	t.Run("non-manifest subject is skipped silently", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := memory.New()

		subject := ociImageSpecV1.Descriptor{
			MediaType: ociImageSpecV1.MediaTypeImageLayer,
			Digest:    digest.FromBytes([]byte("raw")),
			Size:      3,
		}
		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "raw-blob", Version: "1.0.0"},
			},
		}
		r.NoError(materializeOwnershipReferrer(t, ctx, store, subject, resource, "c", "1.0.0"))

		predecessors, err := store.Predecessors(ctx, subject)
		r.NoError(err)
		assert.Empty(t, predecessors, "no referrer must be written when the subject is not an OCI manifest")
	})
}

func TestMarshalArtifactAnnotation(t *testing.T) {
	t.Run("name and version only", func(t *testing.T) {
		out, err := marshalArtifactAnnotation(
			runtime.Identity{"name": "x", "version": "1.0.0"},
			annotations.ArtifactKindResource,
		)
		require.NoError(t, err)
		assert.Equal(t,
			`{"identity":{"name":"x","version":"1.0.0"},"kind":"resource"}`,
			out,
		)
	})

	t.Run("includes extraIdentity keys in canonical key order", func(t *testing.T) {
		// architecture and os are extraIdentity values that ToIdentity() merges
		// into the base name/version. Output must order keys alphabetically so
		// the manifest digest stays stable across runs.
		out, err := marshalArtifactAnnotation(
			runtime.Identity{"version": "1.0.0", "os": "linux", "name": "my-resource", "architecture": "amd64"},
			annotations.ArtifactKindResource,
		)
		require.NoError(t, err)
		assert.Equal(t,
			`{"identity":{"architecture":"amd64","name":"my-resource","os":"linux","version":"1.0.0"},"kind":"resource"}`,
			out,
		)
	})

	t.Run("output is byte-stable regardless of map literal order", func(t *testing.T) {
		a, err := marshalArtifactAnnotation(
			runtime.Identity{"name": "x", "version": "1", "architecture": "amd64", "os": "linux"},
			annotations.ArtifactKindResource,
		)
		require.NoError(t, err)
		b, err := marshalArtifactAnnotation(
			runtime.Identity{"os": "linux", "architecture": "amd64", "version": "1", "name": "x"},
			annotations.ArtifactKindResource,
		)
		require.NoError(t, err)
		assert.Equal(t, a, b)
	})
}
