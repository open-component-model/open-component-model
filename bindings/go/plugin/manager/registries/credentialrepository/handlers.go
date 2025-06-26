package credentialrepository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConsumerIdentityForConfigHandlerFunc is a wrapper around calling the interface method ConsumerIdentityForConfig for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func ConsumerIdentityForConfigHandlerFunc[T runtime.Typed](f func(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[T]) (runtime.Identity, error), scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := plugins.DecodeJSONRequestBody[v1.ConsumerIdentityForConfigRequest[T]](writer, request)
		if err != nil {
			plugins.NewError(fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest).Write(writer)
			return
		}

		identity, err := f(request.Context(), *body)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(identity); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

// ResolveHandlerFunc is a wrapper around calling the interface method Resolve for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func ResolveHandlerFunc[T runtime.Typed](f func(ctx context.Context, cfg v1.ResolveRequest[T], credentials map[string]string) (map[string]string, error), scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(fmt.Errorf("incorrect authentication header format: %w", err), http.StatusUnauthorized).Write(writer)
			return
		}

		body, err := plugins.DecodeJSONRequestBody[v1.ResolveRequest[T]](writer, request)
		if err != nil {
			plugins.NewError(fmt.Errorf("failed to decode request body: %w", err), http.StatusBadRequest).Write(writer)
			return
		}

		resolvedCredentials, err := f(request.Context(), *body, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(resolvedCredentials); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}
