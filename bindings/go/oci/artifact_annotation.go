package oci

import (
	"encoding/json"
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

func GetArtifactOCILayerAnnotation(descriptor *ociImageSpecV1.Descriptor) (*ArtifactOCILayerAnnotation, error) {
	annotation, isOCMArtifact := descriptor.Annotations[ArtifactOCILayerAnnotationKey]
	if !isOCMArtifact {
		return nil, ErrArtifactOCILayerAnnotationDoesNotExist
	}
	var artifactAnnotations []*ArtifactOCILayerAnnotation
	if err := json.Unmarshal([]byte(annotation), &artifactAnnotations); err != nil {
		return nil, err
	}
	artifactAnnotation := artifactAnnotations[0]
	return artifactAnnotation, nil
}

func (a *ArtifactOCILayerAnnotation) AddToDescriptor(descriptor *ociImageSpecV1.Descriptor) error {
	annotation, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if descriptor.Annotations == nil {
		descriptor.Annotations = map[string]string{}
	}
	descriptor.Annotations[ArtifactOCILayerAnnotationKey] = string(annotation)
	return nil
}
