package runtime

import (
	"fmt"
)

var scheme = NewScheme(WithAllowUnknown())

// TypedToIdentity projects a Typed to its matchable Identity view via JSON,
// so native structs and Raw values from external plugins match uniformly.
// Errors if converting from the Typed to Identity fails.
func TypedToIdentity(t Typed) (Identity, error) {
	if t == nil {
		return nil, fmt.Errorf("cannot project nil Typed to Identity")
	}
	if id, ok := t.(Identity); ok {
		return id, nil
	}
	raw := Raw{}
	if err := scheme.Convert(t, &raw); err != nil {
		return nil, fmt.Errorf("could not convert Typed to Raw: %w", err)
	}

	identity := Identity{}
	if err := scheme.Convert(&raw, &identity); err != nil {
		return nil, fmt.Errorf("could not convert Raw to Identity: %w", err)
	}

	return identity, nil
}
