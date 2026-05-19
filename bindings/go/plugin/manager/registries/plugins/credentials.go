package plugins

import (
	"fmt"
	"net/http"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialsFromHeader extracts typed credentials from the Authorization header.
// An absent or empty header returns (nil, true). A malformed header writes a 401
// response and returns (nil, false).
func CredentialsFromHeader(w http.ResponseWriter, h http.Header) (runtime.Typed, bool) {
	authHeader := h.Get("Authorization")
	if authHeader == "" {
		return nil, true
	}
	raw := &runtime.Raw{}
	if err := raw.UnmarshalJSON([]byte(authHeader)); err != nil {
		NewError(fmt.Errorf("incorrect authentication header format: %w", err), http.StatusUnauthorized).Write(w)
		return nil, false
	}
	return raw, true
}
