package blobtransformer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TransformBlobHandlerFunc is a wrapper around calling the interface method TransformBlobHandler for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func TransformBlobHandlerFunc[T runtime.Typed](f func(ctx context.Context, request *v1.TransformBlobRequest[T], credentials map[string]string) (*v1.TransformBlobResponse, error)) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(fmt.Errorf("incorrect authentication header format: %w", err), http.StatusUnauthorized).Write(writer)
			return
		}

		body, err := plugins.DecodeJSONRequestBody[v1.TransformBlobRequest[T]](writer, request)
		if err != nil {
			slog.Error("failed to decode request body", "error", err)
			return
		}
		response, err := f(request.Context(), body, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(response); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

// GetIdentityHandlerFunc creates an HTTP handler for retrieving identity information.
// It handles request processing and response encoding for the plugin implementation.
func GetIdentityHandlerFunc[T runtime.Typed](f func(ctx context.Context, typ *v1.GetIdentityRequest[T]) (*v1.GetIdentityResponse, error), scheme *runtime.Scheme, proto T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		response, err := f(request.Context(), &v1.GetIdentityRequest[T]{
			Typ: proto,
		})
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(response); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}
