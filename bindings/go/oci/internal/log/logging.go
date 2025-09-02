package log

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Operation is a helper function to log operations with timing and error handling.
func Operation(ctx context.Context, operation string, fields ...slog.Attr) func(error) {
	start := time.Now()
	logger := slogcontext.FromCtx(ctx).With(slog.String("realm", "oci"), slog.String("operation", operation))
	logger.LogAttrs(ctx, slog.LevelDebug, "operation starting", fields...)

	return func(err error) {
		duration := slog.Duration("duration", time.Since(start))

		var msg string
		level := slog.LevelDebug
		if err != nil {
			msg = "operation failed"
			fields = append(fields, slog.String("error", err.Error()))
		} else {
			msg = "operation completed"
		}

		logger.LogAttrs(ctx, level, msg, append([]slog.Attr{duration}, fields...)...)
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
