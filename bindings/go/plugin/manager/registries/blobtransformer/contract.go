package blobtransformer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/transformer"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type BlobTransformer interface {
	// TransformBlob transforms the given blob based on the provided transformation type.
	TransformBlob(ctx context.Context, blob blob.ReadOnlyBlob, spec runtime.Typed, credentials map[string]string) (blob.ReadOnlyBlob, error)
	// GetBlobTransformerCredentialConsumerIdentity retrieves an identity for the given specification that
	// can be used to lookup credentials for the blob transformer.
	GetBlobTransformerCredentialConsumerIdentity(ctx context.Context, spec runtime.Typed) (runtime.Identity, error)
}

// The BuiltinBlobTransformer has the primary purpose to allow plugin
// registries to register internal plugins without requiring callers to
// explicitly provide a scheme with their supported types.
// A scheme is mapping types to their go types. As the go types of external
// plugins are not compiled in, they cannot have a scheme and therefore, cannot
// implement this interface.
type BuiltinBlobTransformer interface {
	transformer.Transformer
	GetTransformerScheme() *runtime.Scheme
}
