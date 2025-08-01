package transformer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Transformer transforms blob data according to specified configuration.
// It provides a flexible interface for transforming blob content while maintaining
// compatibility with the existing blob.ReadOnlyBlob interface.
//
// Different implementations can be chosen based on the media type of the input blob
// to provide specialized transformation logic for specific content types.
type Transformer interface {
	// TransformBlob transforms the given blob data according to the specified configuration.
	// It returns the transformed data as a blob.ReadOnlyBlob or an error if the transformation fails.
	TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, config runtime.Typed, credentials map[string]string) (blob.ReadOnlyBlob, error)
}
