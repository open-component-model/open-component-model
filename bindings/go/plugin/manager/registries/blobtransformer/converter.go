package blobtransformer

import (
	"context"
	"errors"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/transformer"
	blobtransformerv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobs"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type converter struct {
	externalPlugin blobtransformerv1.BlobTransformerPluginContract[runtime.Typed]
	scheme         *runtime.Scheme
}

func (c *converter) GetTransformerScheme() *runtime.Scheme {
	return runtime.NewScheme(runtime.WithAllowUnknown())
}

func (c *converter) TransformBlob(ctx context.Context, blob blob.ReadOnlyBlob, spec runtime.Typed, credentials map[string]string) (_ blob.ReadOnlyBlob, err error) {
	tmp, err := os.CreateTemp("", "blob")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() {
		err = errors.Join(err, tmp.Close())
	}()

	if err := filesystem.CopyBlobToOSPath(blob, tmp.Name()); err != nil {
		return nil, fmt.Errorf("failed to copy blob to OS path: %w", err)
	}

	request := &blobtransformerv1.TransformBlobRequest[runtime.Typed]{
		Location: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
		},
		Specification: spec,
	}

	response, err := c.externalPlugin.TransformBlob(ctx, request, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to transform blob: %w", err)
	}

	return blobs.CreateBlobData(response.Location)
}

func (c *converter) GetBlobTransformerCredentialConsumerIdentity(ctx context.Context, spec runtime.Typed) (runtime.Identity, error) {
	request := &blobtransformerv1.GetIdentityRequest[runtime.Typed]{
		Typ: spec,
	}

	result, err := c.externalPlugin.GetIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

var _ transformer.Transformer = (*converter)(nil)

func (r *Registry) externalToBlobTransformerConverter(plugin blobtransformerv1.BlobTransformerPluginContract[runtime.Typed], scheme *runtime.Scheme) *converter {
	return &converter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
