package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
)

func Test_Integration_CTF_Untag(t *testing.T) {
	t.Parallel()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	provider := ocictf.NewFromCTF(archive)
	ctx := t.Context()

	store, err := provider.StoreForReference(ctx, "test-repo:v1.0.0")
	require.NoError(t, err)

	data := []byte(`{"schemaVersion":2}`)
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	require.NoError(t, store.Push(ctx, desc, bytes.NewReader(data)))
	require.NoError(t, store.Tag(ctx, desc, "v1.0.0"))
	require.NoError(t, store.Tag(ctx, desc, "latest"))

	untagger, ok := store.(content.Untagger)
	require.True(t, ok, "CTF store must implement content.Untagger")

	t.Run("removes tag without affecting the sibling tag or the blob", func(t *testing.T) {
		_, err := store.Resolve(ctx, "latest")
		require.NoError(t, err, "latest must resolve before untagging")

		require.NoError(t, untagger.Untag(ctx, "latest"))

		_, err = store.Resolve(ctx, "latest")
		require.ErrorIs(t, err, errdef.ErrNotFound, "untagged reference must not resolve")

		resolved, err := store.Resolve(ctx, "v1.0.0")
		require.NoError(t, err, "sibling tag must still resolve")
		require.Equal(t, desc.Digest, resolved.Digest)

		exists, err := store.Exists(ctx, desc)
		require.NoError(t, err)
		require.True(t, exists, "underlying blob must survive untagging")
	})

	t.Run("returns ErrNotFound for a tag that does not exist", func(t *testing.T) {
		err := untagger.Untag(ctx, "nonexistent")
		require.ErrorIs(t, err, errdef.ErrNotFound)
	})
}

func Test_Integration_RemoteRegistry_Untag(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)

	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_STORAGE_DELETE_ENABLED": "true",
			"REGISTRY_VALIDATION_DISABLED":    "true",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r := require.New(t)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	client := createAuthClient(registryAddress, testUsername, password)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	ref := func(tag string) string {
		return fmt.Sprintf("%s/test-repo:%s", registryAddress, tag)
	}

	store, err := resolver.StoreForReference(ctx, ref("v1.0.0"))
	r.NoError(err)

	// Push a minimal valid OCI manifest so the registry accepts it.
	configData := []byte("{}")
	configDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configData),
		Size:      int64(len(configData)),
	}
	r.NoError(store.Push(ctx, configDesc, bytes.NewReader(configData)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ociImageSpecV1.Descriptor{},
	}
	manifestData, err := json.Marshal(manifest)
	r.NoError(err)
	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestData),
		Size:      int64(len(manifestData)),
	}
	r.NoError(store.Push(ctx, manifestDesc, bytes.NewReader(manifestData)))
	r.NoError(store.Tag(ctx, manifestDesc, "v1.0.0"))
	r.NoError(store.Tag(ctx, manifestDesc, "latest"))

	untagger, ok := store.(content.Untagger)
	r.True(ok, "remote store must implement content.Untagger")

	t.Run("removes tag without affecting the sibling tag", func(t *testing.T) {
		r := require.New(t)
		r.NoError(untagger.Untag(ctx, "latest"))

		_, err := store.Resolve(ctx, ref("latest"))
		r.ErrorIs(err, errdef.ErrNotFound, "untagged reference must not resolve")

		resolved, err := store.Resolve(ctx, ref("v1.0.0"))
		r.NoError(err, "sibling tag must still resolve")
		r.Equal(manifestDesc.Digest, resolved.Digest)
	})

	t.Run("returns ErrNotFound for a tag that does not exist", func(t *testing.T) {
		err := untagger.Untag(ctx, "nonexistent")
		require.ErrorIs(t, err, errdef.ErrNotFound)
	})
}
