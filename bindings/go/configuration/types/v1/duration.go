package v1

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strconv"
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

// MarshalJSONTo writes the duration as a Go duration string.
func (d Duration) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(time.Duration(d).String()))
}

// UnmarshalJSONFrom reads a Go duration string or an integer number of
// nanoseconds. A JSON null leaves the current value untouched.
func (d *Duration) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return fmt.Errorf("failed to parse duration: %w", err)
	}
	switch val.Kind() {
	case 'n':
		return nil
	case '"':
		var str string
		if err := json.Unmarshal(val, &str); err != nil {
			return fmt.Errorf("failed to parse duration: %w", err)
		}
		parsed, err := time.ParseDuration(str)
		if err != nil {
			return fmt.Errorf("invalid duration %q: must be a duration like 30s, 5m or 1h30m, or a number of nanoseconds: %w", str, err)
		}
		*d = Duration(parsed)
		return nil
	case '0':
		// Nanoseconds are integers; decoding them as float64 would lose precision.
		ns, err := strconv.ParseInt(string(val), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid duration %s: a number must be an integer of nanoseconds within int64: %w", val, err)
		}
		*d = Duration(ns)
		return nil
	default:
		return fmt.Errorf("duration must be a string like 30s or a number of nanoseconds, got %s", val)
	}
}
