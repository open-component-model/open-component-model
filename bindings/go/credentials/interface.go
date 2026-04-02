package credentials

import (
	"context"
	"errors"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNotFound is returned when no credentials could be found for the given identity.
var ErrNotFound = errors.New("credentials not found")

// ErrUnknown is a generic error indicating an unknown failure during credential resolution.
var ErrUnknown = errors.New("unknown error occurred")

// Resolver defines the interface for resolving credentials based on a given identity.
// It provides two methods:
//   - Resolve returns credentials as map[string]string (backward compat, will be removed)
//   - ResolveTyped returns credentials as runtime.Typed (preferred, supports typed credential specs)
//
// In case of an error it will either return ErrNotFound when no credentials could be found
// or another error indicating the failure reason wrapped by ErrUnknown.
type Resolver interface {
	// TODO(matthiasbruns): Remove once all consumers use ResolveTyped https://github.com/open-component-model/ocm-project/issues/980
	Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error)
	ResolveTyped(ctx context.Context, identity runtime.Identity) (runtime.Typed, error)
}
