package transformer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TransformBlob transforms a blob by selecting the appropriate transformer
// based on the blob's media type using a simple switch statement.
func TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, config runtime.Typed) (blob.ReadOnlyBlob, error) {
	mediaType := getMediaType(input)

	var transformer blob.BlobTransformer
	switch mediaType {
	case MediaTypeHelmChart, MediaTypeHelmProvenance, MediaTypeHelmConfig:
		transformer = NewHelmTransformer()
	default:
		transformer = NewOCIArtifactTransformer()
	}

	return transformer.TransformBlob(ctx, input, config)
}

// getMediaType extracts the media type from a blob
func getMediaType(input blob.ReadOnlyBlob) string {
	if mediaTypeAware, ok := input.(blob.MediaTypeAware); ok {
		if mediaType, known := mediaTypeAware.MediaType(); known {
			return mediaType
		}
	}
	return "application/octet-stream" // default fallback
}
