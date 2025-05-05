package log

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Base() *slog.Logger {
	return slog.With(slog.String("realm", "oci"))
}

// Operation is a helper function to log operations with timing and error handling.
func Operation(ctx context.Context, operation string, fields ...slog.Attr) func(error) {
	start := time.Now()
	logger := Base().With(slog.String("operation", operation))
	logger.LogAttrs(ctx, slog.LevelInfo, "starting operation", fields...)
	return func(err error) {
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "operation failed", slog.Duration("duration", time.Since(start)), slog.String("error", err.Error()))
		} else {
			logger.LogAttrs(ctx, slog.LevelInfo, "operation completed", slog.Duration("duration", time.Since(start)))
		}
	}
}

// DescriptorLogAttr creates a log attribute for an OCI descriptor.
func DescriptorLogAttr(descriptor ociImageSpecV1.Descriptor) slog.Attr {
	args := []any{
		slog.String("mediaType", descriptor.MediaType),
		slog.String("digest", descriptor.Digest.String()),
		slog.Int64("size", descriptor.Size),
	}
	if descriptor.ArtifactType != "" {
		args = append(args, slog.String("artifactType", descriptor.ArtifactType))
	}
	return slog.Group("descriptor", args...)
}

func IdentityLogAttr(group string, identity runtime.Identity) slog.Attr {
	var args []any
	for key := range slices.Values(slices.Sorted(maps.Keys(identity))) {
		args = append(args, slog.String(key, identity[key]))
	}
	return slog.Group(group, args...)
}
