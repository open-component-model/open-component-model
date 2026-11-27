package v1

import (
	"fmt"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// NewNormalizedConfig returns the config blob of the normalized manifest: a component config whose
// componentDescriptorLayer is the normalized layer. Readers that predate this layout follow the
// config to that layer and reject its media type instead of failing on an empty config, and the
// config is fully determined by the normalized layer, which keeps the manifest digest stable.
func NewNormalizedConfig(normalizedLayer ociImageSpecV1.Descriptor) ([]byte, ociImageSpecV1.Descriptor, error) {
	return componentConfig.New(normalizedLayer)
}

// BuildNormalizedManifest builds the normalized (access-free) manifest M. Its single layer is the
// normalized descriptor blob and its config is the one returned by NewNormalizedConfig. It is the
// tag target and the subject other referrers (access descriptor, cosign signatures) attach to.
// subject is the component index when versions are tracked by referrers, and nil otherwise.
// All inputs are derived from the normalized descriptor, the component identity and the
// repository's tracking policy, so the digest of M is stable across copies.
func BuildNormalizedManifest(normalizedLayer, config ociImageSpecV1.Descriptor, subject *ociImageSpecV1.Descriptor, component, version string) ociImageSpecV1.Manifest {
	return ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: ocidescriptor.ArtifactTypeNormalizedDescriptor,
		Config:       config,
		Layers:       []ociImageSpecV1.Descriptor{normalizedLayer},
		Subject:      subject,
		Annotations: map[string]string{
			AnnotationLayoutVersion:          LayoutVersion,
			AnnotationNormalisationAlgo:      NormalisationAlgorithm,
			annotations.OCMComponentVersion:  annotations.NewComponentVersionAnnotation(component, version),
			ociImageSpecV1.AnnotationVersion: version,
			ociImageSpecV1.AnnotationTitle:   fmt.Sprintf("OCM Normalized Component Descriptor for %s in version %s", component, version),
		},
	}
}

// BuildAccessManifest builds the manifest carrying the full access-bearing descriptor layer plus
// the local-blob layers and the component config. With a subject (the normalized manifest) it is
// the access referrer A itself; without one it is the first manifest of the access index built by
// BuildAccessIndex. It never carries the component version annotation, so it is never listed as a
// component version.
func BuildAccessManifest(subject *ociImageSpecV1.Descriptor, config, descriptorLayer ociImageSpecV1.Descriptor, localBlobLayers []ociImageSpecV1.Descriptor, component, version string) ociImageSpecV1.Manifest {
	return ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: ocidescriptor.ArtifactTypeAccessDescriptor,
		Config:       config,
		Subject:      subject,
		Layers:       append([]ociImageSpecV1.Descriptor{descriptorLayer}, localBlobLayers...),
		Annotations: map[string]string{
			AnnotationLayoutVersion:          LayoutVersion,
			ociImageSpecV1.AnnotationVersion: version,
			ociImageSpecV1.AnnotationTitle:   fmt.Sprintf("OCM Access Component Descriptor for %s in version %s", component, version),
		},
	}
}

// BuildAccessIndex builds the access referrer A as an image index for component versions with
// OCI-native local blobs (nested manifests or indexes). Mirroring the v2 index, its first manifest
// is the access manifest (see BuildAccessManifest, without subject), followed by the local-blob
// manifests.
func BuildAccessIndex(subject, accessManifest ociImageSpecV1.Descriptor, localBlobManifests []ociImageSpecV1.Descriptor, component, version string) ociImageSpecV1.Index {
	return ociImageSpecV1.Index{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageIndex,
		ArtifactType: ocidescriptor.ArtifactTypeAccessDescriptor,
		Subject:      &subject,
		Manifests:    append([]ociImageSpecV1.Descriptor{accessManifest}, localBlobManifests...),
		Annotations: map[string]string{
			AnnotationLayoutVersion:          LayoutVersion,
			ociImageSpecV1.AnnotationVersion: version,
			ociImageSpecV1.AnnotationTitle:   fmt.Sprintf("OCM Access Component Descriptor Index for %s in version %s", component, version),
		},
	}
}
