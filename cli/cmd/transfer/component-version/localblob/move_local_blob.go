package localblob

import (
	"context"
	"fmt"
	"log/slog"
	sync "sync"
	"time"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	MoveLocalBlobsTypeString = "MoveLocalBlobs"
	Version                  = "v1alpha1"
)

var Scheme = runtime.NewScheme()

var MoveLocalBlobsV1Alpha1 = runtime.NewVersionedType(MoveLocalBlobsTypeString, Version)

func init() {
	Scheme.MustRegisterWithAlias(&MoveLocalBlobs{}, MoveLocalBlobsV1Alpha1)
}

// MoveLocalBlobs is a transformer specification to move local blob resources
// from one repository to another.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type MoveLocalBlobs struct {
	// +ocm:jsonschema-gen:enum=MoveLocalBlobs/v1alpha1
	Type   runtime.Type          `json:"type"`
	ID     string                `json:"id,omitempty"`
	Spec   *MoveLocalBlobsSpec   `json:"spec"`
	Output *MoveLocalBlobsOutput `json:"output,omitempty"`
}

// MoveLocalBlobsSpec is the specification of the input specification for MoveLocalBlobs.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MoveLocalBlobsSpec struct {
	Descriptor *v2.Descriptor `json:"descriptor,omitempty"`

	From *runtime.Raw `json:"from"`
	To   *runtime.Raw `json:"to"`
}

// MoveLocalBlobsOutput is the output specification of MoveLocalBlobs.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type MoveLocalBlobsOutput struct {
	Descriptor *v2.Descriptor `json:"descriptor,omitempty"`
}

// MoveLocalBlobsTransformer is a transformer to move local blob resources
type MoveLocalBlobsTransformer struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

// Transform moves local blob resources from one repository to another.
func (t *MoveLocalBlobsTransformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var tr MoveLocalBlobs
	if err := t.Scheme.Convert(step, &tr); err != nil {
		return nil, fmt.Errorf("convert transformation: %w", err)
	}

	fromRepo, err := t.getRepository(ctx, tr.Spec.From)
	if err != nil {
		return nil, fmt.Errorf("resolve from repository: %w", err)
	}
	toRepo, err := t.getRepository(ctx, tr.Spec.To)
	if err != nil {
		return nil, fmt.Errorf("resolve to repository: %w", err)
	}

	component := tr.Spec.Descriptor.Component
	resources := component.Resources

	eg, ctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	for i, res := range resources {
		eg.Go(func() error {
			start := time.Now()
			slog.InfoContext(ctx, "transferring local blob resource", "resource", res.ToIdentity())
			defer func() {
				slog.InfoContext(ctx, "transferred local blob resource", "resource", res.ToIdentity(), "duration", time.Since(start))
			}()
			v2res, err := t.moveResource(ctx, component, &res, fromRepo, toRepo)
			if err != nil {
				return fmt.Errorf("could not move local resource %q: %w", res.ToIdentity(), err)
			}
			mu.Lock()
			defer mu.Unlock()
			if v2res == nil {
				slog.InfoContext(ctx, "resource is not a local blob, skipping", "resource", res.ToIdentity())
				return nil
			}
			component.Resources[i] = *v2res
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("transfer local blobs: %w", err)
	}

	tr.Output = &MoveLocalBlobsOutput{
		Descriptor: tr.Spec.Descriptor,
	}

	return &tr, nil
}

// moveResource moves a single local blob resource from one repository to another.
func (t *MoveLocalBlobsTransformer) moveResource(
	ctx context.Context,
	component v2.Component,
	res *v2.Resource,
	fromRepo, toRepo repository.ComponentVersionRepository,
) (*v2.Resource, error) {
	var lb v2.LocalBlob
	if err := v2.Scheme.Convert(res.Access, &lb); err != nil || lb.Type.IsEmpty() {
		// not a local blob, skip
		return nil, nil
	}

	blob, updatedRes, err := fromRepo.GetLocalResource(ctx, component.Name, component.Version, res.ToIdentity())
	if err != nil {
		return nil, fmt.Errorf("get local resource %q: %w", res.ToIdentity(), err)
	}

	updatedRes.Access = &lb

	updated, err := toRepo.AddLocalResource(ctx, component.Name, component.Version, updatedRes, blob)
	if err != nil {
		return nil, fmt.Errorf("add local resource %q: %w", res.ToIdentity(), err)
	}

	v2res, err := descriptor.ConvertToV2Resource(v2.Scheme, updated)
	if err != nil {
		return nil, fmt.Errorf("convert updated resource %q: %w", res.ToIdentity(), err)
	}
	return v2res, nil
}

// getRepository resolves a repository from a raw specification.
func (t *MoveLocalBlobsTransformer) getRepository(
	ctx context.Context,
	raw *runtime.Raw,
) (repository.ComponentVersionRepository, error) {
	spec, err := oci.Scheme.NewObject(raw.GetType())
	if err != nil {
		return nil, err
	}
	if err := oci.Scheme.Convert(raw, spec); err != nil {
		return nil, err
	}

	var creds map[string]string
	if id, err := t.RepoProvider.
		GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, spec); err == nil {
		creds, err = t.CredentialProvider.Resolve(ctx, id)
		if err != nil {
			return nil, err
		}
	}

	return t.RepoProvider.GetComponentVersionRepository(ctx, spec, creds)
}
