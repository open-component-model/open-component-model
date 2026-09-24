package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeStrict decodes `from` into `into`, rejecting any field that `into` does not declare.
// It is the shared strict-decoding primitive used for user-authored typed data.
//
// Plain json.Unmarshal drops unknown fields silently, which turns a misspelled or
// misplaced field into a silently ignored one and can leave the target struct empty.
//
// The decoder reports the first unknown field it meets, not all of them.
func DecodeStrict(from Typed, into Typed) error {
	data, err := json.Marshal(from)
	if err != nil {
		return fmt.Errorf("type %q cannot be encoded for strict decoding: %w", from.GetType(), err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("type %q has unknown or invalid fields: %w", from.GetType(), err)
	}

	return nil
}
