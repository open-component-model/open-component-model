package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type Handler struct {
	Handler  http.HandlerFunc `json:"-"` // ignore the handler when marshalling
	Location string           `json:"location"`
	Schema   []byte           `json:"schema"`
}

type ReadWriteComponentVersionRepositoryHandlers struct {
	UploadComponentVersion   Handler // maybe contain the type here?
	DownloadComponentVersion Handler
	UploadResource           Handler
	DownloadResource         Handler
}

// GetComponentVersionFn these functions provide the structure that plugins need to implement.
type GetComponentVersionFn[T runtime.Typed] func(ctx context.Context, name, version string, registry T, credentials runtime.Identity, writer io.Writer) (err error)
type PostComponentVersionFn[T runtime.Typed] func(ctx context.Context, descriptor *descriptor.Descriptor, registry T, credentials runtime.Identity) error
type GetResourceFn[T runtime.Typed] func(ctx context.Context, request *GetResourceRequest, credentials runtime.Identity, writer io.Writer) error
type PostResourceFn[T runtime.Typed] func(ctx context.Context, request *PostResourceRequest, credentials runtime.Identity, writer io.Writer) error

type ReadWriteComponentVersionRepositoryHandlersOpts[T runtime.Typed] struct {
	UploadComponentVersion PostComponentVersionFn[T]
	GetComponentVersion    GetComponentVersionFn[T]
	UploadResource         PostResourceFn[T]
	DownloadResource       GetResourceFn[T]
}

func (o *ReadWriteComponentVersionRepositoryHandlers) GetHandlers() []Handler {
	return []Handler{
		o.UploadComponentVersion,
		o.DownloadComponentVersion,
		o.UploadResource,
		o.DownloadResource,
	}
}

var _ CapabilityHandlerProvider = &ReadWriteComponentVersionRepositoryHandlers{}

// CapabilityHandlerProvider can be used to list handlers that the plugin SDK needs to register for a plugin.
// This is used by the SDK as a convenience so users don't have to care about it.
type CapabilityHandlerProvider interface {
	GetHandlers() []Handler
}

type ReadWriteComponentVersionRepositoryOptions struct {
	Handlers ReadWriteComponentVersionRepositoryHandlers `json:"handlers"`
}

func NewReadWriteComponentVersionRepository[T runtime.Typed](typ T, pluginType PluginType, handlers ReadWriteComponentVersionRepositoryHandlersOpts[T]) (CapabilityHandlerProvider, []byte, error) {
	//ociRegistry := &OCIRegistry{} // Pretend this is defined in bindings.
	//schemaOCIRegistry, err := jsonschema.Reflect(ociRegistry).MarshalJSON()
	//if err != nil {
	//	return nil, nil, err
	//}

	result := &ReadWriteComponentVersionRepositoryHandlers{
		UploadComponentVersion: Handler{
			Handler:  UploadComponentVersionHandlerFunc(handlers.UploadComponentVersion, typ),
			Location: "/cv/upload", // These should be coming from somewhere because we need to call them later.
		},
		DownloadComponentVersion: Handler{
			Handler:  DownloadComponentVersionHandlerFunc(handlers.GetComponentVersion, typ),
			Location: "/cv/download",
		},
		UploadResource: Handler{
			Handler:  PostResourceHandlerFunc[T](handlers.UploadResource),
			Location: "/cv/upload/resource",
			//Schema:   schemaOCIRegistry,
		},
		DownloadResource: Handler{
			Handler:  GetResourceHandlerFunc[T](handlers.DownloadResource),
			Location: "/cv/download/resource",
			//Schema:   schemaOCIRegistry,
		},
	}

	capability := Capabilities{
		PluginType: pluginType,
		Capabilities: []Capability{
			{
				// TODO: Need the endpoints here? Should the endpoints contain the type?
				Capability: "ReadWriteComponentVersionRepository",
				Type:       typ.GetType().String(),
			},
		},
	}

	content, err := json.Marshal(capability)
	if err != nil {
		return nil, nil, err
	}

	return result, content, nil
}

var Scheme = runtime.NewScheme()

func DownloadComponentVersionHandlerFunc[T runtime.Typed](f GetComponentVersionFn[T], typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		name := query.Get("name")
		version := query.Get("version")
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := runtime.Identity{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}
		if err := Scheme.Decode(strings.NewReader(request.Header.Get(XOCMRepositoryHeader)), typ); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), name, version, typ, credentials, writer); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func UploadComponentVersionHandlerFunc[T runtime.Typed](f PostComponentVersionFn[T], typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		req, err := DecodeJSONRequestBody[PostComponentVersionRequest](writer, request)
		if err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := runtime.Identity{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}
		if err := Scheme.Convert(req.Repository.Typed, typ); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), req.Descriptor, typ, credentials); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func GetResourceHandlerFunc[T runtime.Typed](f GetResourceFn[T]) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		slog.Info("GET request")
		query := request.URL.Query()
		targetLocation := Location{
			LocationType: LocationType(query.Get("target_location_type")),
			Value:        query.Get("target_location_value"),
		}

		res := &descriptor.Resource{}
		if v := query.Get("resource"); v != "" {
			decodedResource, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				NewError(err, http.StatusBadRequest).Write(writer)
				return
			}

			if err := json.Unmarshal(decodedResource, &res); err != nil {
				NewError(err, http.StatusBadRequest).Write(writer)
				return
			}
		}

		req := &GetResourceRequest{
			Resource:       res,
			TargetLocation: targetLocation,
		}

		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := runtime.Identity{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), req, credentials, writer); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func PostResourceHandlerFunc[T runtime.Typed](f PostResourceFn[T]) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := DecodeJSONRequestBody[PostResourceRequest](writer, request)
		if err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := runtime.Identity{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), body, credentials, writer); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
		}
	}
}

func DecodeJSONRequestBody[T any](writer http.ResponseWriter, request *http.Request) (*T, error) {
	pRequest := new(T)
	if err := json.NewDecoder(request.Body).Decode(pRequest); err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}
	return pRequest, nil
}

type Error struct {
	Err    error `json:"error"`
	Status int   `json:"status"`
}

func NewError(err error, status int) *Error {
	return &Error{Err: err, Status: status}
}

func (e *Error) Write(w http.ResponseWriter) {
	http.Error(w, e.Err.Error(), e.Status)
}
