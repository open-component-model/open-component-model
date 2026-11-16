package plugins

import (
	"encoding/json/v2"
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// MarshalJSON encodes v as the JSON body of a plugin response.
//
// Marshaling straight into the [http.ResponseWriter] would flush the first chunk - and
// with it commit a 200 - before a failure half way through the value is noticed, so the
// client would receive a truncated object with the error text appended to it and report
// a JSON syntax error instead of the plugin's actual failure. Encoding to a buffer first
// keeps a failed response an error-only 500.
func MarshalJSON(v any) ([]byte, error) {
	return json.Marshal(v, runtime.JSONOptions)
}

// WriteJSON writes v as the JSON body of a plugin response, replacing it with a 500
// carrying the marshal error if v cannot be encoded. See [MarshalJSON].
func WriteJSON(w http.ResponseWriter, v any) {
	data, err := MarshalJSON(v)
	if err != nil {
		NewError(err, http.StatusInternalServerError).Write(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // the status is committed; a failed write leaves the client with a short body it will reject
	w.Write(data)
}
