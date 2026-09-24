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

func TestWeakEdgeFailurePolicy(t *testing.T) {
	newStore := func(t *testing.T) *memory.Store {
		store := memory.New()
		pushTestBlob(t, store, ociImageSpecV1.MediaTypeEmptyJSON, ociImageSpecV1.DescriptorEmptyJSON.Data)
		return store
	}
	layer := func(t *testing.T, store *memory.Store) []ociImageSpecV1.Descriptor {
		return []ociImageSpecV1.Descriptor{pushTestBlob(t, store, "application/vnd.test.layer", []byte("layer"))}
	}

	tests := []struct {
		name string
		// setup returns the source store and the copy root.
		setup      func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor)
		abortFails bool
		skipFails  bool
	}{
		{
			name: "missing subject",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				store := newStore(t)
				subject := missingDescriptor("subject")
				return store, pushTestManifest(t, store, layer(t, store), &subject)
			},
			abortFails: true,
		},
		{
			name: "missing referrer",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				store := newStore(t)
				root := pushTestManifest(t, store, layer(t, store), nil)
				return &ghostReferrers{Store: store, subject: root, ghost: missingDescriptor("referrer")}, root
			},
			abortFails: true,
		},
		{
			name: "missing layer",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				store := newStore(t)
				return store, pushTestManifest(t, store, []ociImageSpecV1.Descriptor{missingDescriptor("layer")}, nil)
			},
			abortFails: true,
			skipFails:  true,
		},
		{
			name: "complete graph with subject",
			setup: func(t *testing.T) (oras.ReadOnlyGraphTarget, ociImageSpecV1.Descriptor) {
				store := newStore(t)
				layers := layer(t, store)
				subject := pushTestManifest(t, store, layers, nil)
				return store, pushTestManifest(t, store, layers, &subject)
			},
		},
	}

	for _, tc := range tests {
		for policyName, policy := range map[string]WeakEdgeFailurePolicy{"abort": WeakEdgeFailurePolicyAbort, "skip": WeakEdgeFailurePolicySkip} {
			wantErr := tc.abortFails
			if policy == WeakEdgeFailurePolicySkip {
				wantErr = tc.skipFails
			}
			t.Run(tc.name+"/"+policyName, func(t *testing.T) {
				r := require.New(t)
				src, root := tc.setup(t)
				dst := memory.New()
				repo := &Repository{weakEdgeFailurePolicy: policy}

				err := oras.ExtendedCopyGraph(t.Context(), src, dst, root, repo.extendedCopyGraphOptions())
				if wantErr {
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
}
