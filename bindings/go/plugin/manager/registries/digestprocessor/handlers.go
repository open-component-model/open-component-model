package digestprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceDigestProcessorHandlerFunc is a wrapper around calling the interface method ProcessResourceDigest for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func ResourceDigestProcessorHandlerFunc(f func(ctx context.Context, request *v1.ProcessResourceDigestRequest, credentials map[string]string) (*v1.ProcessResourceDigestResponse, error)) http.HandlerFunc {
	return digestProcessorHandlerFunc[v1.ProcessResourceDigestRequest, v1.ProcessResourceDigestResponse](f)
}

// IdentityProcessorHandlerFunc is a wrapper around calling the interface method GetIdentity for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func IdentityProcessorHandlerFunc(f func(ctx context.Context, typ *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error)) http.HandlerFunc {
	return identityProcessorHandlerFunc[v1.GetIdentityRequest[runtime.Typed], v1.GetIdentityResponse](f)
}

func digestProcessorHandlerFunc[REQ, RES any](f func(ctx context.Context, resource *REQ, credentials map[string]string) (*RES, error)) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
		logger.Info("request", "request", request.Method, "url", request.URL.String())

		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(fmt.Errorf("failed to marshal credentials: %w", err), http.StatusUnauthorized).Write(writer)
			return
		}

		defer request.Body.Close()

		req := new(REQ)
		if err := json.NewDecoder(request.Body).Decode(req); err != nil {
			plugins.NewError(fmt.Errorf("failed to marshal request body: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}

		resp, err := f(request.Context(), req, credentials)
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

func identityProcessorHandlerFunc[REQ, RES any](f func(ctx context.Context, typ *REQ) (*RES, error)) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
		logger.Info("request", "request", request.Method, "url", request.URL.String())

		defer request.Body.Close()

		result := new(REQ)
		if err := json.NewDecoder(request.Body).Decode(result); err != nil {
			plugins.NewError(fmt.Errorf("failed to marshal request body: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}

		resp, err := f(request.Context(), result)
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
