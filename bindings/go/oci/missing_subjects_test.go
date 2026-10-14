package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/oci/tar"
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

// ghostReferrers lists a referrer without content for the subject.
type ghostReferrers struct {
	*memory.Store
	subject, ghost ociImageSpecV1.Descriptor
}

func (g *ghostReferrers) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	predecessors, err := g.Store.Predecessors(ctx, desc)
	if err != nil || desc.Digest != g.subject.Digest {
		return predecessors, err
	}
	return append(predecessors, g.ghost), nil
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
				return &ghostReferrers{Store: store, subject: root, ghost: missingDescriptor("referrer")}, root
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

// mediaTypeStrictStore mirrors ORAS remote repository semantics, which
// [memory.Store] does not: Exists is digest-based, and Fetch rejects a
// descriptor whose media type differs from the stored content's.
type mediaTypeStrictStore struct {
	*memory.Store
	byDigest map[digest.Digest]ociImageSpecV1.Descriptor
}

func (s *mediaTypeStrictStore) Exists(ctx context.Context, d ociImageSpecV1.Descriptor) (bool, error) {
	stored, ok := s.byDigest[d.Digest]
	if !ok {
		return false, nil
	}
	return s.Store.Exists(ctx, stored)
}

func (s *mediaTypeStrictStore) Fetch(ctx context.Context, d ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	stored, ok := s.byDigest[d.Digest]
	if !ok {
		return nil, fmt.Errorf("%s: %w", d.Digest, errdef.ErrNotFound)
	}
	if stored.MediaType != d.MediaType {
		return nil, fmt.Errorf("mismatch response Content-Type %q: expect %q", stored.MediaType, d.MediaType)
	}
	return s.Store.Fetch(ctx, stored)
}

// TestExtendedCopyFollowsSubjectWithSourceDescriptor covers a referrer whose
// subject states the wrong media type for its digest, as the Notary signature
// of docker.io/bitnamilegacy/kubectl:1.30.2 does for its manifest list.
func TestExtendedCopyFollowsSubjectWithSourceDescriptor(t *testing.T) {
	const (
		mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
		mediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	)

	setup := func(t *testing.T) (src *mediaTypeStrictStore, root, referrer, wrongSubject ociImageSpecV1.Descriptor) {
		t.Helper()
		store := memory.New()
		config := pushTestBlob(t, store, ociImageSpecV1.MediaTypeEmptyJSON, ociImageSpecV1.DescriptorEmptyJSON.Data)
		layers := []ociImageSpecV1.Descriptor{pushTestBlob(t, store, "application/vnd.test.layer", []byte("layer"))}
		child := pushTestManifest(t, store, layers, nil)

		data, err := json.Marshal(ociImageSpecV1.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: mediaTypeDockerManifestList,
			Manifests: []ociImageSpecV1.Descriptor{child},
		})
		require.NoError(t, err)
		root = pushTestBlob(t, store, mediaTypeDockerManifestList, data)

		wrongSubject = root
		wrongSubject.MediaType = mediaTypeDockerManifest
		referrer = pushTestManifest(t, store, layers, &wrongSubject)

		src = &mediaTypeStrictStore{Store: store, byDigest: map[digest.Digest]ociImageSpecV1.Descriptor{}}
		for _, d := range []ociImageSpecV1.Descriptor{config, layers[0], child, root, referrer} {
			src.byDigest[d.Digest] = d
		}
		return src, root, referrer, wrongSubject
	}

	tests := []struct {
		name string
		copy func(t *testing.T, repo *Repository, src *mediaTypeStrictStore, dst *memory.Store, root ociImageSpecV1.Descriptor) error
	}{
		{
			name: "direct",
			copy: func(t *testing.T, repo *Repository, src *mediaTypeStrictStore, dst *memory.Store, root ociImageSpecV1.Descriptor) error {
				t.Helper()
				return oras.ExtendedCopyGraph(t.Context(), src, dst, root, repo.extendedCopyGraphOptions())
			},
		},
		{
			name: "via OCI layout",
			copy: func(t *testing.T, repo *Repository, src *mediaTypeStrictStore, dst *memory.Store, root ociImageSpecV1.Descriptor) error {
				t.Helper()
				b, err := tar.CopyToOCILayoutInMemory(t.Context(), src, root, tar.CopyToOCILayoutOptions{
					ExtendedCopyGraphOptions: repo.extendedCopyGraphOptions(),
					TempDir:                  t.TempDir(),
				})
				if err != nil {
					return err
				}
				_, err = tar.CopyOCILayoutWithIndex(t.Context(), dst, b, tar.CopyOCILayoutWithIndexOptions{
					ExtendedCopyGraphOptions: repo.extendedCopyGraphOptions(),
				})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			src, root, referrer, wrongSubject := setup(t)
			dst := memory.New()

			r.NoError(tc.copy(t, &Repository{}, src, dst, root))

			for _, want := range []struct {
				desc   ociImageSpecV1.Descriptor
				exists bool
			}{{root, true}, {referrer, true}, {wrongSubject, false}} {
				exists, err := dst.Exists(t.Context(), want.desc)
				r.NoError(err)
				r.Equal(want.exists, exists, "%s %s", want.desc.MediaType, want.desc.Digest)
			}
		})
	}
}
