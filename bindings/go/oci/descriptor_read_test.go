package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/content/memory"
	"github.com/stretchr/testify/require"

	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// Manifests are bounded by the blob cache limit, not by the 4 MiB default of oras.FetchBytes.
func TestGetDescriptorOCIImageManifest_ReadsLargeManifests(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	store := memory.New()

	config := ociImageSpecV1.DescriptorEmptyJSON
	r.NoError(store.Push(ctx, config, bytes.NewReader(config.Data)))
	body, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ociImageSpecV1.MediaTypeImageManifest,
		Config:      config,
		Layers:      []ociImageSpecV1.Descriptor{},
		Annotations: map[string]string{"large": strings.Repeat("x", 5<<20)},
	})
	r.NoError(err)
	desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, body)
	r.NoError(store.Push(ctx, desc, bytes.NewReader(body)))
	r.NoError(store.Tag(ctx, desc, "v1"))

	manifest, index, err := getDescriptorOCIImageManifest(ctx, store, "v1")
	r.NoError(err)
	r.Nil(index)
	r.Len(manifest.Annotations["large"], 5<<20)
}

// forgingStore serves forged bytes in place of the blob with digest forge.
type forgingStore struct {
	*memory.Store
	forge  digest.Digest
	forged []byte
}

func (s *forgingStore) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	if target.Digest == s.forge {
		return io.NopCloser(bytes.NewReader(s.forged)), nil
	}
	return s.Store.Fetch(ctx, target)
}

func TestGetDescriptorFromStore_RejectsForgedBlobs(t *testing.T) {
	ctx := t.Context()
	descriptorRaw := []byte("component:\n  name: ocm.software/test\n")
	descriptorLayer := content.NewDescriptorFromBytes(ocidescriptor.MediaTypeComponentDescriptorYAML, descriptorRaw)
	configRaw, err := json.Marshal(componentConfig.Config{ComponentDescriptorLayer: &descriptorLayer})
	require.NoError(t, err)
	config := content.NewDescriptorFromBytes(componentConfig.MediaType, configRaw)

	for _, tc := range []struct {
		name   string
		forge  ociImageSpecV1.Descriptor
		forged []byte
	}{
		{name: "config", forge: config, forged: bytes.Replace(configRaw, []byte(descriptorLayer.Digest.Encoded()[:8]), []byte("00000000"), 1)},
		{name: "descriptor layer", forge: descriptorLayer, forged: bytes.Replace(descriptorRaw, []byte("test"), []byte("evil"), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			r.Len(tc.forged, int(tc.forge.Size), "the forgery must only be caught by its digest")
			mem := memory.New()
			r.NoError(mem.Push(ctx, descriptorLayer, bytes.NewReader(descriptorRaw)))
			r.NoError(mem.Push(ctx, config, bytes.NewReader(configRaw)))
			body, err := json.Marshal(ociImageSpecV1.Manifest{
				Versioned: specs.Versioned{SchemaVersion: 2},
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Config:    config,
				Layers:    []ociImageSpecV1.Descriptor{descriptorLayer},
			})
			r.NoError(err)
			manifest := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, body)
			r.NoError(mem.Push(ctx, manifest, bytes.NewReader(body)))
			r.NoError(mem.Tag(ctx, manifest, "v1"))

			store := &forgingStore{Store: mem, forge: tc.forge.Digest, forged: tc.forged}
			_, _, _, err = getDescriptorFromStore(ctx, store, "v1", nil)
			r.ErrorIs(err, content.ErrMismatchedDigest)
		})
	}
}
