package componentversionrepository

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// decodeJSONRequestBody takes a request and any type and decodes the request's body into that type
// and returns the result.
func decodeJSONRequestBody[T any](writer http.ResponseWriter, request *http.Request) (*T, error) {
	pRequest := new(T)
	if err := json.NewDecoder(request.Body).Decode(pRequest); err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}
	return pRequest, nil
}
