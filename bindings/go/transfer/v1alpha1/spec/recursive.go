package spec

import (
	"encoding/json"
	"fmt"
)

// Recursive controls whether component references are transferred along with
// their parent component, and how deep that recursion goes.
//
// In the wire format it accepts either an integer or a boolean:
//   - an integer sets an explicit depth: -1 means infinite recursion, 0 means
//     no recursion, and n > 0 limits recursion to n levels;
//   - a boolean is shorthand: true maps to infinite recursion (-1), false to
//     no recursion (0).
//
// It always marshals back out as an integer.
//
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=schemas/Recursive.schema.json
type Recursive int

const (
	// RecursiveNone disables recursion; only the parent component is transferred.
	RecursiveNone Recursive = 0

	// RecursiveInfinite recurses through all component references without limit.
	RecursiveInfinite Recursive = -1
)

func (r Recursive) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(r))
}

func (r *Recursive) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("failed to parse recursive: %w", err)
	}

	switch value := v.(type) {
	case bool:
		if value {
			*r = RecursiveInfinite
		} else {
			*r = RecursiveNone
		}
		return nil
	case float64:
		if value != float64(int(value)) {
			return fmt.Errorf("recursive must be a whole number, got %v", value)
		}
		*r = Recursive(value)
		return nil
	default:
		return fmt.Errorf("recursive must be a boolean or an integer, got %T", v)
	}
}
