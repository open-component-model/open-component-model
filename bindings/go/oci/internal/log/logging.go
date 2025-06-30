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

// Base returns a base logger for OCI operations with a default RealmKey.
func Base() *slog.Logger {
	return slog.With(slog.String("realm", "oci"))
}

// Operation is a helper function to log operations with timing and error handling.
func Operation(ctx context.Context, operation string, fields ...slog.Attr) func(error) {
	start := time.Now()
	logger := Base().With(slog.String("operation", operation))
	logger.LogAttrs(ctx, slog.LevelInfo, "operation starting", fields...)

	return func(err error) {
		duration := slog.Duration("duration", time.Since(start))

		var level slog.Level
		var msg string
		if err != nil {
			level, msg = slog.LevelError, "operation failed"
			fields = append(fields, slog.String("error", err.Error()))
		} else {
			level, msg = slog.LevelInfo, "operation completed"
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
