package runtime

import (
	"encoding/json"
	"fmt"
)

// TypedToIdentity projects any Typed value (concrete struct, in-process plugin
// type, or Raw bytes from an out-of-process plugin) into the canonical
// matchable Identity map view.
//
// The projection round-trips the value through JSON, so it is uniformly
// available to any Typed regardless of whether the host has the concrete Go
// type registered. This is what allows Raw values produced by external
// (possibly non-Go) plugins to participate in identity matching alongside
// native typed structs.
//
// The projection is intentionally strict: a Typed whose JSON form is not a
// flat object of string-valued fields returns an error. Non-string scalars,
// arrays, and nested objects all surface as a contract violation rather than
// silently mismatching at compare time.
//
// TypedToIdentity is the only bridge needed for matching. It is not a
// general-purpose serialization API; use json.Marshal for that.
func TypedToIdentity(t Typed) (Identity, error) {
	if t == nil {
		return nil, fmt.Errorf("cannot project nil Typed to Identity")
	}
	data, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("could not marshal typed value to JSON: %w", err)
	}

	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("typed value does not project to a flat object: %w", err)
	}

	identity := make(Identity, len(raw))
	for key, value := range raw {
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			return nil, fmt.Errorf("typed value field %q is not a string: %w", key, err)
		}
		identity[key] = s
	}
	return identity, nil
}
