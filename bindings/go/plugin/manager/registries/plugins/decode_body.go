package plugins

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// DecodeJSONRequestBody takes a request and any type and decodes the request's body into that type
// and returns the result.
func DecodeJSONRequestBody[T any](writer http.ResponseWriter, request *http.Request) (*T, error) {
	pRequest := new(T)
	if err := json.NewDecoder(request.Body).Decode(pRequest); err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}
	return pRequest, nil
}
