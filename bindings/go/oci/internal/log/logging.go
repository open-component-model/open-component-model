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

// loggerContextKey is the key for storing loggers in context.
type loggerContextKey struct{}

// WithLogger returns a context with the logger attached.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// FromContext returns the logger from the context, or nil if not found.
func FromContext(ctx context.Context) *slog.Logger {
	if logger := ctx.Value(loggerContextKey{}); logger != nil {
		return logger.(*slog.Logger)
	}
	return nil
}

// Base returns a base logger for OCI operations with a default RealmKey.
func Base(ctx context.Context) *slog.Logger {
	if logger := FromContext(ctx); logger != nil {
		return logger
	}

	return slog.With(slog.String("realm", "oci"))
}

// Operation is a helper function to log operations with timing and error handling.
func Operation(ctx context.Context, operation string, fields ...slog.Attr) func(error) {
	start := time.Now()
	logger := Base(ctx).With(slog.String("operation", operation))
	logger.LogAttrs(ctx, slog.LevelDebug, "operation starting", fields...)

	return func(err error) {
		duration := slog.Duration("duration", time.Since(start))

		var level slog.Level
		var msg string
		if err != nil {
			level, msg = slog.LevelError, "operation failed"
			fields = append(fields, slog.String("error", err.Error()))
		} else {
			level, msg = slog.LevelDebug, "operation completed"
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
