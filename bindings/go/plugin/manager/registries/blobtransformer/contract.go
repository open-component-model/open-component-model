package blobtransformer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// BlobTransformer defines the transformation contract for plugins to implement.
type BlobTransformer interface {
	// TransformBlob transforms the given blob based on the provided transformation type.
	TransformBlob(ctx context.Context, blob blob.ReadOnlyBlob, spec runtime.Typed, credentials map[string]string) (blob.ReadOnlyBlob, error)
	// GetBlobTransformerCredentialConsumerIdentity retrieves an identity for the given specification that
	// can be used to lookup credentials for the blob transformer.
	GetBlobTransformerCredentialConsumerIdentity(ctx context.Context, spec runtime.Typed) (runtime.Identity, error)
}
