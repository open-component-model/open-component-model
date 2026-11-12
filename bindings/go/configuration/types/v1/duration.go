package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a [time.Duration] in configuration. It marshals as a Go duration
// string such as 30s, 5m or 1h30m, and unmarshals from such a string or from
// an integer number of nanoseconds.
//
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=schemas/Duration.schema.json
type Duration time.Duration

// NewDuration returns a pointer to d as a [Duration].
func NewDuration(d time.Duration) *Duration {
	v := Duration(d)
	return &v
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	// Numbers are integer nanoseconds; decoding them as float64 would lose precision.
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("failed to parse duration: %w", err)
	}
	switch value := v.(type) {
	case json.Number:
		ns, err := value.Int64()
		if err != nil {
			return fmt.Errorf("invalid duration %s: a number must be an integer of nanoseconds within int64: %w", value, err)
		}
		*d = Duration(ns)
		return nil
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration %q: must be a duration like 30s, 5m or 1h30m, or a number of nanoseconds: %w", value, err)
		}
		*d = Duration(parsed)
		return nil
	default:
		return fmt.Errorf("duration must be a string like 30s or a number of nanoseconds, got %T", v)
	}
}
