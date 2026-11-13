package plugins

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
)

// DecodeJSONRequestBody takes a request and any type and decodes the request's body into that type
// and returns the result.
func DecodeJSONRequestBody[T any](writer http.ResponseWriter, request *http.Request) (*T, error) {
	pRequest := new(T)
	if err := json.UnmarshalRead(request.Body, pRequest); err != nil {
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}
	return pRequest, nil
}
