package runtime

import (
	"context"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Transformer is the base interface for all transformation step executors.
// The runtime dispatches each graph node to the registered Transformer for its type.
// Implementations that also need resolved credentials should implement
// [TransformerWithCredentials] instead; the runtime checks for that interface first
// and falls back to Transformer when it is not present.
type Transformer interface {
	// Transform executes the transformation step described by step and returns
	// the updated step with any output fields populated.
	Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error)
}

// TransformerWithCredentials is an opt-in extension for [Transformer] implementations
// that require credential resolution per step.
//
// The runtime resolves credentials as follows:
//  1. Calls [GetCredentialConsumerIdentities] to obtain the named identity slots
//     declared by this transformer for the given step.
//  2. For each slot, calls the configured credentials.Resolver with the slot's identity.
//     Slots whose identity resolves to credentials.ErrNotFound are silently omitted.
//     Any other resolver error aborts the transformation.
//  3. Passes the resulting map to [TransformWithCredentials].
//     The map is always non-nil; it is empty when no credentials are found or no
//     resolver is configured.
//
// Transformers that hold a credentials.Resolver directly do not need to implement
// this interface.
type TransformerWithCredentials interface {
	// GetCredentialConsumerIdentities returns the named set of consumer identity
	// slots required by this transformer for the given step. The slot names (map
	// keys) are arbitrary but must be stable — the same names are used as keys in
	// the credentials map passed to TransformWithCredentials.
	// Returning an empty map is valid; TransformWithCredentials will be called with
	// an empty credentials map.
	GetCredentialConsumerIdentities(ctx context.Context, step runtime.Typed) (map[string]runtime.Identity, error)

	// TransformWithCredentials executes the transformation step with the resolved
	// credentials. The credentials map is keyed by the slot names returned from
	// GetCredentialConsumerIdentities; slots that could not be resolved are absent.
	// Implementations must tolerate a missing slot and decide locally whether to
	// proceed without those credentials or return an error.
	TransformWithCredentials(ctx context.Context, step runtime.Typed, credentials map[string]runtime.Typed) (runtime.Typed, error)
}
