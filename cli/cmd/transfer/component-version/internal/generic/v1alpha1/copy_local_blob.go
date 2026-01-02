package v1alpha1

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CopyLocalBlobVersionType = "CopyLocalBlob"

// CopyLocalBlob is a transformer specification to copy local resources between OCM repositories.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type CopyLocalBlob struct {
	// +ocm:jsonschema-gen:enum=CopyLocalBlob/v1alpha1
	Type   runtime.Type         `json:"type"`
	ID     string               `json:"id,omitempty"`
	Spec   *CopyLocalBlobSpec   `json:"spec"`
	Output *CopyLocalBlobOutput `json:"output,omitempty"`
}

// CopyLocalBlobOutput is the output specification of
// CopyLocalBlob.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CopyLocalBlobOutput struct {
	Resource *v2.Resource `json:"resource"`
}

// CopyLocalBlobSpec is the specification to copy a local blob from one location to another.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type CopyLocalBlobSpec struct {
	From      *runtime.Raw     `json:"from"`
	To        *runtime.Raw     `json:"to"`
	Component string           `json:"component"`
	Version   string           `json:"version"`
	Resource  runtime.Identity `json:"resource"`
}

type CopyLocalBlobImpl struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *CopyLocalBlobImpl) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	copyLocalBlob := &CopyLocalBlob{}
	if err := t.Scheme.Convert(step, copyLocalBlob); err != nil {
		return nil, err
	}

	var fromCreds, toCreds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, copyLocalBlob.Spec.From); err == nil {
			if fromCreds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, copyLocalBlob.Spec.To); err == nil {
			if toCreds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	sourceRepo, err := t.RepoProvider.GetComponentVersionRepository(ctx, copyLocalBlob.Spec.From, fromCreds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}
	targetRepo, err := t.RepoProvider.GetComponentVersionRepository(ctx, copyLocalBlob.Spec.To, toCreds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}

	// TODO either implement specific point to point transformers or implement transformer optimization
	//   based on implementation. We can decide here if we want to have the graph generate optimized transformers
	//   and have a higher level type safety, or if we want to have a more powerful transformer.
	blob, res, err := sourceRepo.GetLocalResource(ctx, copyLocalBlob.Spec.Component, copyLocalBlob.Spec.Version, copyLocalBlob.Spec.Resource)
	if err != nil {
		return nil, fmt.Errorf("failed getting local resource: %w", err)
	}

	acc := res.GetAccess()
	var localBlob v2.LocalBlob
	if err := v2.Scheme.Convert(acc, &localBlob); err != nil {
		return nil, fmt.Errorf("failed to convert artifact access to local blob: %w", err)
	}
	res.Access = &localBlob

	res, err = targetRepo.AddLocalResource(ctx, copyLocalBlob.Spec.Component, copyLocalBlob.Spec.Version, res, blob)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource: %w", err)
	}

	v2res, err := descriptor.ConvertToV2Resource(runtime.NewScheme(runtime.WithAllowUnknown()), res)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2: %w", err)
	}

	copyLocalBlob.Output = &CopyLocalBlobOutput{Resource: v2res}

	return copyLocalBlob, nil
}
