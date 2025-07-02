package input

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceInputProcessorHandlerFunc is a wrapper around calling the interface method ProcessResource for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func ResourceInputProcessorHandlerFunc(f func(ctx context.Context, r *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceInputResponse, error), scheme *runtime.Scheme, typ runtime.Typed) http.HandlerFunc {
	return inputProcessorHandlerFunc[v1.ProcessResourceInputRequest, v1.ProcessResourceInputResponse](f)
}

// SourceInputProcessorHandlerFunc is a wrapper around calling the interface method ProcessSource for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func SourceInputProcessorHandlerFunc(f func(ctx context.Context, r *v1.ProcessSourceInputRequest, credentials map[string]string) (*v1.ProcessSourceInputResponse, error), scheme *runtime.Scheme, typ runtime.Typed) http.HandlerFunc {
	return inputProcessorHandlerFunc[v1.ProcessSourceInputRequest, v1.ProcessSourceInputResponse](f)
}

func inputProcessorHandlerFunc[REQ, RES any](f func(ctx context.Context, r *REQ, credentials map[string]string) (*RES, error)) http.HandlerFunc {
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

		result := new(REQ)
		if err := json.NewDecoder(request.Body).Decode(result); err != nil {
			plugins.NewError(fmt.Errorf("failed to marshal request body: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}

		resp, err := f(request.Context(), result, credentials)
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
