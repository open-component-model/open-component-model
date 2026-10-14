package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"
)

func pushTestBlob(t *testing.T, store *memory.Store, mediaType string, data []byte) ociImageSpecV1.Descriptor {
	t.Helper()
	desc := ociImageSpecV1.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(data), Size: int64(len(data))}
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))
	return desc
}

func pushTestManifest(t *testing.T, store *memory.Store, layers []ociImageSpecV1.Descriptor, subject *ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor {
	t.Helper()
	data, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    ociImageSpecV1.DescriptorEmptyJSON,
		Layers:    layers,
		Subject:   subject,
	})
	require.NoError(t, err)
	return pushTestBlob(t, store, ociImageSpecV1.MediaTypeImageManifest, data)
}

func missingDescriptor(name string) ociImageSpecV1.Descriptor {
	return ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Digest: digest.FromString(name), Size: 42}
}

// listedReferrer lists referrer for subject in addition to the referrers the
// store derives from pushed manifests. The referrer's content may be absent.
type listedReferrer struct {
	*memory.Store
	subject, referrer ociImageSpecV1.Descriptor
}

func (l *listedReferrer) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	predecessors, err := l.Store.Predecessors(ctx, desc)
	if err != nil || desc.Digest != l.subject.Digest {
		return predecessors, err
	}
	return append(predecessors, l.referrer), nil
}

func TestCopySkipsMissingSubjects(t *testing.T) {
	newStore := func(t *testing.T) *memory.Store {
		t.Helper()
		store := memory.New()
		pushTestBlob(t, store, ociImageSpecV1.MediaTypeEmptyJSON, ociImageSpecV1.DescriptorEmptyJSON.Data)
		return store
	}
	layer := func(t *testing.T, store *memory.Store) []ociImageSpecV1.Descriptor {
		t.Helper()
		return []ociImageSpecV1.Descriptor{pushTestBlob(t, store, "application/vnd.test.layer", []byte("layer"))}
	}

	tests := []struct {
		name string
		// setup returns the source store and the copy root.
		setup   func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor)
		wantErr bool
	}{
		{
			name: "missing subject",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				t.Helper()
				store := newStore(t)
				subject := missingDescriptor("subject")
				return store, pushTestManifest(t, store, layer(t, store), &subject)
			},
		},
		{
			name: "missing subject of artifact manifest",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				t.Helper()
				store := newStore(t)
				data, err := json.Marshal(map[string]any{
					"mediaType": mediaTypeArtifactManifest,
					"blobs":     layer(t, store),
					"subject":   missingDescriptor("subject"),
				})
				require.NoError(t, err)
				return store, pushTestBlob(t, store, mediaTypeArtifactManifest, data)
			},
		},
		{
			name: "missing referrer",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				t.Helper()
				store := newStore(t)
				root := pushTestManifest(t, store, layer(t, store), nil)
				return &listedReferrer{Store: store, subject: root, referrer: missingDescriptor("referrer")}, root
			},
		},
		{
			name: "missing layer",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				t.Helper()
				store := newStore(t)
				return store, pushTestManifest(t, store, []ociImageSpecV1.Descriptor{missingDescriptor("layer")}, nil)
			},
			wantErr: true,
		},
		{
			name: "complete graph with subject",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				t.Helper()
				store := newStore(t)
				layers := layer(t, store)
				subject := pushTestManifest(t, store, layers, nil)
				return store, pushTestManifest(t, store, layers, &subject)
			},
		},
		{
			// The referrer declares its index subject as a single manifest, as
			// the Notary signature of docker.io/bitnamilegacy/kubectl:1.30.2 does.
			name: "referrer declares subject with wrong media type",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				t.Helper()
				store := newStore(t)
				layers := layer(t, store)
				child := pushTestManifest(t, store, layers, nil)
				data, err := json.Marshal(ociImageSpecV1.Index{
					Versioned: specs.Versioned{SchemaVersion: 2},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{child},
				})
				require.NoError(t, err)
				root := pushTestBlob(t, store, ociImageSpecV1.MediaTypeImageIndex, data)
				declared := root
				declared.MediaType = ociImageSpecV1.MediaTypeImageManifest
				signature := pushTestManifest(t, store, layers, &declared)
				return &listedReferrer{Store: store, subject: root, referrer: signature}, root
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			src, root := tc.setup(t)
			dst := memory.New()
			repo := &Repository{}

			err := oras.ExtendedCopyGraph(t.Context(), src, dst, root, repo.extendedCopyGraphOptions())
			if tc.wantErr {
				r.ErrorIs(err, errdef.ErrNotFound)
				return
			}
			r.NoError(err)
			exists, err := dst.Exists(t.Context(), root)
			r.NoError(err)
			r.True(exists)
		})
	}
}
