package v1

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

const MediaType = "application/vnd.ocm.software.component-index.v1+json"

var Manifest = ociImageSpecV1.Manifest{
	Versioned: specs.Versioned{
		SchemaVersion: 2,
	},
	MediaType:    ociImageSpecV1.MediaTypeImageManifest,
	ArtifactType: MediaType,
	Config:       ociImageSpecV1.DescriptorEmptyJSON,
	Layers: []ociImageSpecV1.Descriptor{
		ociImageSpecV1.DescriptorEmptyJSON,
	},
	Annotations: map[string]string{
		"software.ocm.description": "This is an OCM component index. It is an empty json" +
			"that can be used as referrer for OCM component descriptors. It is used as a subject" +
			"for all OCM Component Version Top-Level Manifests and can be used to reference back all" +
			"OCM Component Versions",
	},
}

var Descriptor = ociImageSpecV1.Descriptor{
	MediaType:    Manifest.MediaType,
	ArtifactType: Manifest.ArtifactType,
	Digest:       "sha256:03e3d2a4051bec7aef98dd78c26d8b1d9079161aa3d92ec9161669b45f6ea486",
	Size:         767,
}

type Store interface {
	content.ReadOnlyStorage
	content.Pusher
}

func CreateIfNotExists(ctx context.Context, store Store) error {
	if exists, err := store.Exists(ctx, Manifest.Layers[0]); err != nil {
		return err
	} else if !exists {
		if err := store.Push(ctx, Manifest.Layers[0], bytes.NewReader(Manifest.Layers[0].Data)); err != nil {
			return err
		}
	}

	exists, err := store.Exists(ctx, Descriptor)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Create the index if it does not exist
	indexRaw, err := json.Marshal(Manifest)
	if err != nil {
		return err
	}
	if err := store.Push(ctx, Descriptor, bytes.NewReader(indexRaw)); err != nil {
		return err
	}

	return nil
}
