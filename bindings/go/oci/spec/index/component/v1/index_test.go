package v1

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"
)

func Test_Manifest(t *testing.T) {
	r := require.New(t)

	data, err := json.Marshal(Manifest)
	r.NoError(err)

	dig := digest.FromBytes(data)

	r.Equal(dig, Descriptor.Digest)
	r.Equal(int64(len(data)), Descriptor.Size)
	r.Equal(Manifest.MediaType, Descriptor.MediaType)
	r.Equal(Manifest.ArtifactType, Descriptor.ArtifactType)
}

func TestCreateIfNotExists(t *testing.T) {
	t.Run("successful creation when index does not exist", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := memory.New()

		// First call should create the index
		err := CreateIfNotExists(ctx, store)
		r.NoError(err)

		// Verify the layer exists
		exists, err := store.Exists(ctx, Manifest.Layers[0])
		r.NoError(err)
		r.True(exists)

		// Verify the descriptor exists
		exists, err = store.Exists(ctx, Descriptor)
		r.NoError(err)
		r.True(exists)
	})

	t.Run("successful when index already exists", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := memory.New()

		// Create the index first
		err := CreateIfNotExists(ctx, store)
		r.NoError(err)

		// Second call should succeed without error
		err = CreateIfNotExists(ctx, store)
		r.NoError(err)

		// Verify everything still exists
		exists, err := store.Exists(ctx, Manifest.Layers[0])
		r.NoError(err)
		r.True(exists)

		exists, err = store.Exists(ctx, Descriptor)
		r.NoError(err)
		r.True(exists)
	})

	t.Run("error when store fails", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		// Create a store that always returns an error
		store := &mockStore{err: assert.AnError}

		err := CreateIfNotExists(ctx, store)
		r.Error(err)
		r.Equal(assert.AnError, err)
	})
}

// mockStore implements the Store interface and always returns an error
type mockStore struct {
	err error
}

func (m *mockStore) Exists(ctx context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	return false, m.err
}

func (m *mockStore) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return nil, m.err
}

func (m *mockStore) Push(ctx context.Context, target ociImageSpecV1.Descriptor, content io.Reader) error {
	return m.err
}
