package constructorrepositroy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/construction/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceInputProcessorHandlerFunc is a wrapper around calling the interface method GetComponentVersion for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func ResourceInputProcessorHandlerFunc(f func(ctx context.Context, request v1.ProcessResourceRequest, credentials map[string]string) (v1.ProcessResourceResponse, error), scheme *runtime.Scheme, typ runtime.Typed) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(fmt.Errorf("failed to marshal credentials: %w", err), http.StatusUnauthorized).Write(writer)
			return
		}
		defer request.Body.Close()

		result := &v1.ProcessResourceRequest{}
		if err := json.NewDecoder(request.Body).Decode(result); err != nil {
			plugins.NewError(fmt.Errorf("failed to marshal request body: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}

		resp, err := f(request.Context(), *result, credentials)
		if err != nil {
			plugins.NewError(fmt.Errorf("failed to call processor function: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(resp); err != nil {
			plugins.NewError(fmt.Errorf("failed to encode response: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}
	}
}
