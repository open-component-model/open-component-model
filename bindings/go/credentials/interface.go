package credentials

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNotFound is returned when no credentials could be found for the given identity.
var ErrNotFound = errors.New("credentials not found")

// ErrUnknown is a generic error indicating an unknown failure during credential resolution.
var ErrUnknown = errors.New("unknown error occurred")

// Resolver defines the interface for resolving credentials based on a given identity.
//
// In case of an error it will either return ErrNotFound when no credentials could be found
// or another error indicating the failure reason wrapped by ErrUnknown.
type Resolver interface {
	// TODO(matthiasbruns): Remove once all consumers use ResolveTyped https://github.com/open-component-model/ocm-project/issues/980
	Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error)
	ResolveTyped(ctx context.Context, identity runtime.Identity) (runtime.Typed, error)
}

// ResolveAs is a generic helper that resolves credentials via a Resolver and
// type-asserts the result to T. This gives call-site type safety without requiring
// generic interfaces (Go does not support type parameters on interface methods).
func ResolveAs[T runtime.Typed](ctx context.Context, r Resolver, identity runtime.Identity) (T, error) {
	var zero T
	typed, err := r.ResolveTyped(ctx, identity)
	if err != nil {
		return zero, err
	}
	out, ok := typed.(T)
	if !ok {
		return zero, fmt.Errorf("credential type mismatch: want %T, got %T (%s): %w", zero, typed, typed.GetType(), ErrUnknown)
	}
	return out, nil
}
