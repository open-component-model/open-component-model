package blobtransformer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type BlobTransformer interface {
	// TransformBlob transforms the given blob based on the provided transformation type.
	TransformBlob(ctx context.Context, blob blob.ReadOnlyBlob, spec runtime.Typed, credentials map[string]string) (blob.ReadOnlyBlob, error)
}
