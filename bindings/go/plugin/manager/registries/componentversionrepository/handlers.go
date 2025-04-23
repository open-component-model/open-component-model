package componentversionrepository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetComponentVersionHandlerFunc is a wrapper around calling the interface method GetComponentVersion for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func GetComponentVersionHandlerFunc[T runtime.Typed](f func(ctx context.Context, request types.GetComponentVersionRequest[T], credentials contracts.Attributes) (*descriptor.Descriptor, error), scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		// Just put this shit into the SDK since it's type agnostic.
		// It's once per contract.
		query := request.URL.Query()
		name := query.Get("name")
		version := query.Get("version")
		rawCredentials := []byte(request.Header.Get("Authorization"))
		// TODO: Replace this with correct Credential Structure
		credentials := contracts.Attributes{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := scheme.Decode(strings.NewReader(request.Header.Get(XOCMRepositoryHeader)), typ); err != nil {
			plugins.NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		desc, err := f(request.Context(), types.GetComponentVersionRequest[T]{
			Repository: typ,
			Name:       name,
			Version:    version,
		}, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(desc); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func AddComponentVersionHandlerFunc[T runtime.Typed](f func(ctx context.Context, request types.PostComponentVersionRequest[T], credentials contracts.Attributes) error) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := plugins.DecodeJSONRequestBody[types.PostComponentVersionRequest[T]](writer, request)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := contracts.Attributes{} // TODO: Change these to contracts.Attributes
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), *body, credentials); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
		}
	}
}

func GetLocalResourceHandlerFunc[T runtime.Typed](f func(ctx context.Context, request types.GetLocalResourceRequest[T], credentials contracts.Attributes) error, scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		name := query.Get("name")
		version := query.Get("version")
		targetLocation := types.Location{
			LocationType: types.LocationType(query.Get("target_location_type")),
			Value:        query.Get("target_location_value"),
		}
		identityQuery := query.Get("identity")
		decodedIdentity, err := base64.StdEncoding.DecodeString(identityQuery)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		identity := map[string]string{}
		if identityQuery != "" {
			if err := json.Unmarshal(decodedIdentity, &identity); err != nil {
				plugins.NewError(err, http.StatusBadRequest).Write(writer)
				return
			}
		}

		if err := scheme.Decode(strings.NewReader(request.Header.Get(XOCMRepositoryHeader)), typ); err != nil {
			plugins.NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := contracts.Attributes{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), types.GetLocalResourceRequest[T]{
			Repository:     typ,
			Name:           name,
			Version:        version,
			Identity:       identity,
			TargetLocation: targetLocation,
		}, credentials); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func AddLocalResourceHandlerFunc[T runtime.Typed](f func(ctx context.Context, request types.PostLocalResourceRequest[T], credentials contracts.Attributes) (*descriptor.Resource, error)) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := plugins.DecodeJSONRequestBody[types.PostLocalResourceRequest[T]](writer, request)
		if err != nil {
			slog.Error("failed to decode request body", "error", err)
			return
		}
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := contracts.Attributes{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		desc, err := f(request.Context(), *body, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(desc); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}
