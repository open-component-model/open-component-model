package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/invopop/jsonschema"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type Handler struct {
	Handler  http.HandlerFunc `json:"-"` // ignore the handler when marshalling
	Location string           `json:"location"`
	Schema   []byte           `json:"schema"`
}

// GetComponentVersionFn these functions provide the structure that plugins need to implement.
type GetComponentVersionFn func(ctx context.Context, name, version string, registry runtime.Typed, credentials Attributes, writer io.Writer) (err error)
type PostComponentVersionFn func(ctx context.Context, descriptor *descriptor.Descriptor, registry runtime.Typed, credentials Attributes) error
type GetResourceFn func(ctx context.Context, request *GetResourceRequest, credentials Attributes, writer io.Writer) error
type PostResourceFn func(ctx context.Context, request *PostResourceRequest, credentials Attributes, writer io.Writer) error

// ReadWriteComponentVersionRepositoryHandlersOpts contains all the functions that the plugin choosing this
// capability has to implement.
type ReadWriteComponentVersionRepositoryHandlersOpts struct {
	UploadComponentVersion PostComponentVersionFn
	GetComponentVersion    GetComponentVersionFn
	UploadResource         PostResourceFn
	DownloadResource       GetResourceFn
}

// CapabilityBuilder constructs a capability for the plugin. Register*Capability will keep updating
// an internal tracker. Once all capabilities have been declared, we call PrintCapabilities to
// return the registered capabilities to the plugin manager.
type CapabilityBuilder struct {
	currentCapabilities Capabilities // schema?
	handlers            []Handler    // now I can gather all of these and the user just has to call `GetHandlers()` and that's it.
}

// NewCapabilityBuilder constructs a new builder for registering capabilities for the given plugin type.
// TODO: We can derive the plugin type from the capability.
// TODO: A single binary should be able to register multiple plugin types.
func NewCapabilityBuilder(pluginType PluginType) *CapabilityBuilder {
	return &CapabilityBuilder{
		currentCapabilities: Capabilities{
			PluginType: pluginType,
		},
	}
}

// PrintCapabilities returns the accumulated capabilities during Register* calls.
func (c *CapabilityBuilder) PrintCapabilities() error {
	content, err := json.Marshal(c.currentCapabilities)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
		return err
	}

	return nil
}

// GetHandlers returns all the handlers that this plugin implemented during the registration of a capability.
func (c *CapabilityBuilder) GetHandlers() []Handler {
	return c.handlers
}

func (c *CapabilityBuilder) RegisterReadWriteComponentVersionRepositoryCapability(
	typ runtime.Typed,
	handlers ReadWriteComponentVersionRepositoryHandlersOpts,
) error {
	schemaOCIRegistry, err := jsonschema.Reflect(typ).MarshalJSON()
	if err != nil {
		return err
	}

	// Setup capabilities
	c.currentCapabilities.Capabilities = append(c.currentCapabilities.Capabilities, Capability{
		Capability: "ReadWriteComponentVersionRepository",
		Type:       typ.GetType().String(),
	})

	// Setup handlers
	c.handlers = append(c.handlers, Handler{
		Handler:  UploadComponentVersionHandlerFunc(handlers.UploadComponentVersion, typ),
		Location: "/cv/upload", // These should be coming from somewhere because we need to call them later.
	},
		Handler{
			Handler:  DownloadComponentVersionHandlerFunc(handlers.GetComponentVersion, typ),
			Location: "/cv/download",
		},
		Handler{
			Handler:  PostResourceHandlerFunc(handlers.UploadResource, schemaOCIRegistry),
			Location: "/cv/upload/resource",
			Schema:   schemaOCIRegistry, // Jakob: this would be derived from the passed in type?
		},
		Handler{
			Handler:  GetResourceHandlerFunc(handlers.DownloadResource, schemaOCIRegistry),
			Location: "/cv/download/resource",
			Schema:   schemaOCIRegistry,
		})

	return nil
}

var Scheme = runtime.NewScheme()

func DownloadComponentVersionHandlerFunc(f GetComponentVersionFn, typ runtime.Typed) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		name := query.Get("name")
		version := query.Get("version")
		rawCredentials := []byte(request.Header.Get("Authorization"))
		// TODO: Replace this with correct Credential Structure
		credentials := Attributes{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}
		if err := Scheme.Decode(strings.NewReader(request.Header.Get(XOCMRepositoryHeader)), typ); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		// TODO: Since this is the actual wrapper around the endpoint I could pass in the Schema here
		// if it exists! THIS could be the location to actually verify the Schema.

		if err := f(request.Context(), name, version, typ, credentials, writer); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func UploadComponentVersionHandlerFunc(f PostComponentVersionFn, typ runtime.Typed) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		req, err := DecodeJSONRequestBody[PostComponentVersionRequest](writer, request)
		if err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}
		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := Attributes{}
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

func GetResourceHandlerFunc(f GetResourceFn, schema []byte) http.HandlerFunc {
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
		credentials := Attributes{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		// TODO: Do the schema validation here?
		// Yes, use the validation schema in here. -> jakob confirmed this
		if err := f(request.Context(), req, credentials, writer); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func PostResourceHandlerFunc(f PostResourceFn, schema []byte) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		body, err := DecodeJSONRequestBody[PostResourceRequest](writer, request)
		if err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := Attributes{} // TODO: Change these to Attributes
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
