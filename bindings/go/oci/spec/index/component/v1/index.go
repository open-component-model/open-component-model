// Package v1 implements the OCM Component Index specification.
// It defines the structure of the index and provides functions to create and manage it.
//
// The Component Index is used in conjunction with the OCI Referrers API.
// Any Component Version pushed to an OCI repository holds a subject reference to its corresponding
// Component Index. This allows for the discovery of all Component Versions associated with a specific index.
//
// See https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers
package v1

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// MediaType defines the media type for OCM Component Index.
const MediaType = "application/vnd.ocm.software.component-index.v1+json"

// Manifest defines the OCI manifest structure for the Component Index.
// It is an empty JSON manifest that serves as a referrer for OCM component descriptors.
// Manifest is hard-coded against Descriptor and MUST never change.
// This is because the Index for the Referrer API must remain stable.
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
		ociImageSpecV1.AnnotationTitle: "OCM Component Index V1",
		ociImageSpecV1.AnnotationDescription: "This is an OCM component index. It is an empty json" +
			"that can be used as referrer for OCM component descriptors. It is used as a subject" +
			"for all OCM Component Version Top-Level Manifests and can be used to reference back all" +
			"OCM Component Versions",
	},
}

// Descriptor represents the OCI descriptor for the Component Index manifest.
// It contains the digest and size of the manifest.
var Descriptor = ociImageSpecV1.Descriptor{
	MediaType:    Manifest.MediaType,
	ArtifactType: Manifest.ArtifactType,
	// Digest is calculated from the Manifest JSON representation and MUST never change.
	Digest: "sha256:9717cda41c478af11cba7ed29f4aa3e4882bab769d006788169cbccafc0fcd05",
	// Size is calculated from the Manifest JSON representation and MUST never change.
	Size: 837,
}

// Store defines the interface for interacting with the OCI content store.
// It combines read-only storage and push capabilities required for managing the Component Index.
type Store interface {
	content.ReadOnlyStorage
	content.Pusher
}

// CreateIfNotExists creates the Component Index in the store if it doesn't already exist.
// It first checks if the layer exists, creates it if needed, then checks if the descriptor exists,
// and finally creates the index manifest if it doesn't exist.
//
// Parameters:
//   - ctx: Context for the operation
//   - store: The content store where the index will be created
//
// Returns:
//   - error: Any error that occurred during the creation process
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
