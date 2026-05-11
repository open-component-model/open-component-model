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
//
// In case of an error it will either return ErrNotFound when no credentials could be found
// or another error indicating the failure reason wrapped by ErrUnknown.
type Resolver interface {
	// Resolve resolves credentials for the given identity and returns them as a map.
	//
	// TODO(matthiasbruns): Remove once all consumers use ResolveTyped https://github.com/open-component-model/ocm-project/issues/980
	//
	// Deprecated: Migrate to ResolveTyped instead for typed credential support.
	Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error)
	// ResolveTyped resolves credentials for the given identity and returns them as a runtime.Typed.
	// The returned value is either a registered typed credential (e.g. *HelmHTTPCredentials) or
	// a *v1.DirectCredentials fallback for legacy Credentials/v1 configs.
	ResolveTyped(ctx context.Context, identity runtime.Identity) (runtime.Typed, error)
}

// CredentialTypeSchemeProvider provides read access to a runtime.Scheme of known credential types.
// The credential graph uses this during ingestion to deserialize typed credentials and resolve
// type aliases. It is optional — when nil, the graph falls back to *v1.DirectCredentials.
type CredentialTypeSchemeProvider interface {
	GetCredentialTypeScheme() *runtime.Scheme
}
