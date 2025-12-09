package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	v2runtime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/transformations/oci"
)

type DownloadComponent struct {
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *DownloadComponent) Transform(
	ctx context.Context,
	step *v1alpha1.GenericTransformation,
) (*v1alpha1.GenericTransformation, error) {
	transformation := &transformv1alpha1.DownloadComponentTransformation{}
	if err := fromGeneric(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to download component transformation: %v", err)
	}

	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, transformation.Spec.Repository); err == nil {
			creds, err = t.CredentialProvider.Resolve(ctx, consumerId)
			if err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %v", err)
			}
		}
	}

	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, transformation.Spec.Repository, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %v", err)
	}
	// TODO(fabianburth): throw an error if one attempts to marshal a runtime
	//  descriptor
	desc, err := repo.GetComponentVersion(ctx, transformation.Spec.Component, transformation.Spec.Version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %v", transformation.Spec.Component, transformation.Spec.Version, err)
	}

	v2desc, err := v2runtime.ConvertToV2(runtime.NewScheme(), desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version to v2: %v", err)
	}
	var m map[string]any
	data, err := json.Marshal(v2desc)
	if err != nil {
		return nil, fmt.Errorf("failed marshalling component version descriptor: %v", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed unmarshalling component version descriptor into map: %v", err)
	}

	// TODO remove hack
	step.Output = &runtime.Unstructured{
		Data: map[string]interface{}{
			"descriptor": m,
		},
	}

	return step, nil
}

func fromGeneric(from *v1alpha1.GenericTransformation, into *transformv1alpha1.DownloadComponentTransformation) error {
	data, err := json.Marshal(from.Spec.Data["repository"])
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	var repo runtime.Raw
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&repo); err != nil {
		return fmt.Errorf("failed to decode strict into runtime raw: %w", err)
	}
	component := from.Spec.Data["component"]
	version := from.Spec.Data["version"]
	if component == nil || version == nil {
		return fmt.Errorf("component and version must be specified in spec data")
	}
	transformation := &transformv1alpha1.DownloadComponentTransformation{
		Type: from.Type,
		ID:   from.ID,
		Spec: &transformv1alpha1.DownloadComponentTransformationSpec{
			Repository: &repo,
			Component:  component.(string),
			Version:    version.(string),
		},
		Output: nil,
	}
	into.Spec = transformation.Spec
	return nil
}
