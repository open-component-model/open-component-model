package input

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
)

type Method interface {
	GetBlob(ctx context.Context, resource *spec.Resource) (data blob.ReadOnlyBlob, err error)
}
