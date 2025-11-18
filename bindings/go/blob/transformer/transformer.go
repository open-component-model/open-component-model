package transformer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Transformer can be used to interact with the blob data and perform operations on it according to the technology-specific
// implementations. For example, introspecting helm archives.
type Transformer interface {
	// TransformBlob transforms the given blob data according to the specified configuration.
	// It returns the transformed data as a blob.ReadOnlyBlob or an error if the transformation fails.
	TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, config runtime.Typed, credentials map[string]string) (blob.ReadOnlyBlob, error)

	// GetTransformerScheme() *runtime.Scheme
}
