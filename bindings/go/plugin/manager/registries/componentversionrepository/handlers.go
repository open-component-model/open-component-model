package componentversionrepository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetComponentVersionHandlerFunc is a wrapper around calling the interface method GetComponentVersion for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func GetComponentVersionHandlerFunc[T runtime.Typed](f func(ctx context.Context, request v1.GetComponentVersionRequest[T], credentials map[string]string) (*descriptor.Descriptor, error), scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		name := query.Get("name")
		version := query.Get("version")
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(fmt.Errorf("incorrect authentication header format: %w", err), http.StatusUnauthorized).Write(writer)
			return
		}

		desc, err := f(request.Context(), v1.GetComponentVersionRequest[T]{
			Repository: typ,
			Name:       name,
			Version:    version,
		}, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		// _Note_: Eventually, this will use a versioned converter.
		descV2, err := descriptor.ConvertToV2(scheme, desc)
		if err != nil {
			plugins.NewError(fmt.Errorf("failed to convert to v2 descriptor: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(descV2); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

// ListComponentVersionsHandlerFunc is a wrapper around calling the interface method ListComponentVersions for the plugin.
// This is a convenience wrapper containing header and query parameter parsing logic that is not important to know for
// the plugin implementor.
func ListComponentVersionsHandlerFunc[T runtime.Typed](f func(ctx context.Context, request v1.ListComponentVersionsRequest[T], credentials map[string]string) ([]string, error), scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		name := query.Get("name")
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(fmt.Errorf("incorrect authentication header format: %w", err), http.StatusUnauthorized).Write(writer)
			return
		}

		versions, err := f(request.Context(), v1.ListComponentVersionsRequest[T]{
			Repository: typ,
			Name:       name,
		}, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(versions); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func AddComponentVersionHandlerFunc[T runtime.Typed](f func(ctx context.Context, r v1.PostComponentVersionRequest[T], credentials map[string]string) error) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(err, http.StatusUnauthorized).Write(writer)
			return
		}
		body, err := plugins.DecodeJSONRequestBody[v1.PostComponentVersionRequest[T]](writer, request)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := f(request.Context(), *body, credentials); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
		}
	}
}

func GetLocalResourceHandlerFunc[T runtime.Typed](f func(ctx context.Context, request v1.GetLocalResourceRequest[T], credentials map[string]string) (v1.GetLocalResourceResponse, error), scheme *runtime.Scheme, proto T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(err, http.StatusUnauthorized).Write(writer)
			return
		}

		query := request.URL.Query()
		name := query.Get("name")
		version := query.Get("version")
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

		response, err := f(request.Context(), v1.GetLocalResourceRequest[T]{
			Repository: proto,
			Name:       name,
			Version:    version,
			Identity:   identity,
		}, credentials)
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

func GetIdentityHandlerFunc[T runtime.Typed](f func(ctx context.Context, typ v1.GetIdentityRequest[T]) (runtime.Identity, error), scheme *runtime.Scheme, proto T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		//rawCredentials := []byte(request.Header.Get("Authorization"))
		//credentials := map[string]string{}
		//if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
		//	plugins.NewError(err, http.StatusUnauthorized).Write(writer)
		//	return
		//}

		// TODO: Figure this out
		//query := request.URL.Query()
		//name := query.Get("name")
		//version := query.Get("version")

		response, err := f(request.Context(), v1.GetIdentityRequest[T]{
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

func AddLocalResourceHandlerFunc[T runtime.Typed](f func(ctx context.Context, request v1.PostLocalResourceRequest[T], credentials map[string]string) (*descriptor.Resource, error), scheme *runtime.Scheme) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := map[string]string{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			plugins.NewError(err, http.StatusUnauthorized).Write(writer)
			return
		}

		body, err := plugins.DecodeJSONRequestBody[v1.PostLocalResourceRequest[T]](writer, request)
		if err != nil {
			slog.Error("failed to decode request body", "error", err)
			return
		}

		resource, err := f(request.Context(), *body, credentials)
		if err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		// _Note_: Eventually, this will use a versioned converter.
		resourceV2, err := descriptor.ConvertToV2Resources(scheme, []descriptor.Resource{*resource})
		if err != nil {
			plugins.NewError(fmt.Errorf("failed to convert to v2 resource: %w", err), http.StatusInternalServerError).Write(writer)
			return
		}

		if len(resourceV2) == 0 {
			plugins.NewError(errors.New("no resources returned during conversion"), http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(resourceV2[0]); err != nil {
			plugins.NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}
