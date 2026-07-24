package v1

import (
	"fmt"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// BuildNormalizedManifest builds the normalized (access-free) manifest M. Its config is the OCI
// empty descriptor and its single layer is the normalized descriptor blob. It is the tag target
// and the subject other referrers (access descriptor, cosign signatures) attach to. Its content
// is fully determined by the normalized layer digest, so its own digest is stable across copies.
func BuildNormalizedManifest(normalizedLayer ociImageSpecV1.Descriptor, component, version string) ociImageSpecV1.Manifest {
	return ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: ocidescriptor.ArtifactTypeNormalizedDescriptor,
		Config:       ociImageSpecV1.DescriptorEmptyJSON,
		Layers:       []ociImageSpecV1.Descriptor{normalizedLayer},
		Annotations: map[string]string{
			AnnotationLayoutVersion:          LayoutVersion,
			AnnotationNormalisationAlgo:      NormalisationAlgorithm,
			ociImageSpecV1.AnnotationVersion: version,
			ociImageSpecV1.AnnotationTitle:   fmt.Sprintf("OCM Normalized Component Descriptor for %s in version %s", component, version),
		},
	}
}

// BuildAccessManifest builds the access-bearing manifest A as a referrer (subject) of the
// normalized manifest. It carries the full v2 descriptor layer plus any local-blob layers and the
// component config. It is regenerated per registry and is never signed.
func BuildAccessManifest(subject, config, descriptorLayer ociImageSpecV1.Descriptor, localBlobLayers []ociImageSpecV1.Descriptor, component, version string) ociImageSpecV1.Manifest {
	subjectCopy := subject
	return ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: ocidescriptor.ArtifactTypeAccessDescriptor,
		Config:       config,
		Subject:      &subjectCopy,
		Layers:       append([]ociImageSpecV1.Descriptor{descriptorLayer}, localBlobLayers...),
		Annotations: map[string]string{
			AnnotationLayoutVersion:          LayoutVersion,
			ociImageSpecV1.AnnotationVersion: version,
			ociImageSpecV1.AnnotationTitle:   fmt.Sprintf("OCM Access Component Descriptor for %s in version %s", component, version),
		},
	}
}
