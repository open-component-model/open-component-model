package annotations

import (
	"encoding/json"
	"errors"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ArtifactKind string

const (
	ArtifactKindSource   ArtifactKind = "source"
	ArtifactKindResource ArtifactKind = "resource"
)

const ArtifactAnnotationKey = "software.ocm.artifact"

var ErrArtifactOCILayerAnnotationDoesNotExist = fmt.Errorf("ocm artifact annotation %s does not exist", ArtifactAnnotationKey)

// ArtifactOCIAnnotation is an annotation that can be added to an OCI layer or manifest to store additional information about the layer.
// It is used to store OCM Artifact information in the layer.
// This is to differentiate Sources and Resources from each other based on their kind.
type ArtifactOCIAnnotation struct {
	Identity runtime.Identity `json:"identity"`
	Kind     ArtifactKind     `json:"kind"`
}

func GetArtifactOCILayerAnnotations(descriptor *ociImageSpecV1.Descriptor) ([]ArtifactOCIAnnotation, error) {
	annotation, isOCMArtifact := descriptor.Annotations[ArtifactAnnotationKey]
	if !isOCMArtifact {
		return nil, ErrArtifactOCILayerAnnotationDoesNotExist
	}
	var artifactAnnotations []ArtifactOCIAnnotation
	if err := json.Unmarshal([]byte(annotation), &artifactAnnotations); err != nil {
		return nil, err
	}
	return artifactAnnotations, nil
}

func (a ArtifactOCIAnnotation) AddToDescriptor(descriptor *ociImageSpecV1.Descriptor) error {
	var annotations []ArtifactOCIAnnotation
	if descriptor.Annotations == nil {
		descriptor.Annotations = map[string]string{}
	} else {
		var err error
		if annotations, err = GetArtifactOCILayerAnnotations(descriptor); err != nil &&
			!errors.Is(err, ErrArtifactOCILayerAnnotationDoesNotExist) {
			return err
		}
	}
	annotations = append(annotations, a)
	annotation, err := json.Marshal(annotations)
	if err != nil {
		return fmt.Errorf("could not marshal artifact annotations: %w", err)
	}

	descriptor.Annotations[ArtifactAnnotationKey] = string(annotation)
	return nil
}

func IsArtifactForResource(descriptor ociImageSpecV1.Descriptor, identity runtime.Identity, kind ArtifactKind, matchers ...runtime.ChainableIdentityMatcher) bool {
	artifactAnnotations, err := GetArtifactOCILayerAnnotations(&descriptor)
	if err != nil {
		return false
	}
	for _, annotation := range artifactAnnotations {
		if annotation.Kind == kind && annotation.Identity.Match(identity, matchers...) {
			return true
		}
	}
	return false
}
