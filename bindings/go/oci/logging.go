package oci

import (
	"context"
	"log/slog"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// logOperation is a helper function to log operations with timing and error handling.
func logOperation(ctx context.Context, operation string, fields ...slog.Attr) func(error) {
	start := time.Now()
	attrs := make([]any, 0, len(fields)+1)
	attrs = append(attrs, slog.String("operation", operation))
	for _, field := range fields {
		attrs = append(attrs, field)
	}
	logger := logger.With(attrs...)
	logger.Log(ctx, slog.LevelInfo, "starting operation")
	return func(err error) {
		if err != nil {
			logger.Log(ctx, slog.LevelError, "operation failed", slog.Duration("duration", time.Since(start)), slog.String("error", err.Error()))
		} else {
			logger.Log(ctx, slog.LevelInfo, "operation completed", slog.Duration("duration", time.Since(start)))
		}
	}
}

// descriptorLogAttr creates a log attribute for an OCI descriptor.
func descriptorLogAttr(descriptor ociImageSpecV1.Descriptor) slog.Attr {
	return slog.Group("descriptor",
		slog.String("mediaType", descriptor.MediaType),
		slog.String("digest", descriptor.Digest.String()),
		slog.Int64("size", descriptor.Size),
	)
}
