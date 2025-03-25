package oci

import (
	"encoding/json"
	"errors"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type ArtifactKind string

const (
	ArtifactKindSource   ArtifactKind = "source"
	ArtifactKindResource ArtifactKind = "resource"
)

const ArtifactOCILayerAnnotationKey = "software.ocm.artifact"

var ErrArtifactOCILayerAnnotationDoesNotExist = fmt.Errorf("ocm artifact annotation %s does not exist", ArtifactOCILayerAnnotationKey)

// ArtifactOCILayerAnnotation is an annotation that can be added to an OCI layer to store additional information about the layer.
// It is used to store OCM Artifact information in the layer.
// This is to differentiate Sources and Resources from each other based on their kind.
type ArtifactOCILayerAnnotation struct {
	Identity map[string]string `json:"identity"`
	Kind     ArtifactKind      `json:"kind"`
}

func GetArtifactOCILayerAnnotations(descriptor *ociImageSpecV1.Descriptor) ([]ArtifactOCILayerAnnotation, error) {
	annotation, isOCMArtifact := descriptor.Annotations[ArtifactOCILayerAnnotationKey]
	if !isOCMArtifact {
		return nil, ErrArtifactOCILayerAnnotationDoesNotExist
	}
	var artifactAnnotations []ArtifactOCILayerAnnotation
	if err := json.Unmarshal([]byte(annotation), &artifactAnnotations); err != nil {
		return nil, err
	}
	return artifactAnnotations, nil
}

func (a ArtifactOCILayerAnnotation) AddToDescriptor(descriptor *ociImageSpecV1.Descriptor) error {
	var annotations []ArtifactOCILayerAnnotation
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

	descriptor.Annotations[ArtifactOCILayerAnnotationKey] = string(annotation)
	return nil
}
