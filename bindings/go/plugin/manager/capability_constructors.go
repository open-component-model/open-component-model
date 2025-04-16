package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Handler contains the handling function, the location and the Schema for wrapping function calls with.
// For example, a real function passed in from the plugin is wrapper into an HTTP HandlerFunc to be called
// later. This handler will use the Schema in order to verify any access spec information that is passed in.
type Handler struct {
	Handler  http.HandlerFunc
	Location string
}

// GetComponentVersionFn these functions provide the structure that plugins need to implement.
// All the types need to be part of the SDK anyway...
type (
	GetComponentVersionFn[T runtime.Typed]  func(ctx context.Context, name, version string, repository T, credentials Attributes, writer io.Writer) (err error)
	PostComponentVersionFn[T runtime.Typed] func(ctx context.Context, descriptor *descriptor.Descriptor, repository T, credentials Attributes) error
	GetResourceFn                           func(ctx context.Context, request *GetResourceRequest, credentials Attributes, writer io.Writer) error
	PostResourceFn                          func(ctx context.Context, request *PostResourceRequest, credentials Attributes, writer io.Writer) error
)

// ReadComponentVersionRepositoryHandlersOpts contains all the functions that the plugin choosing this
// capability has to implement.
type ReadComponentVersionRepositoryHandlersType struct {
	GetComponentVersion GetComponentVersionFn
	DownloadResource    GetResourceFn
}

// CapabilityBuilder constructs a capability for the plugin. Register*Capability will keep updating
// an internal tracker. Once all capabilities have been declared, we call PrintCapabilities to
// return the registered capabilities to the plugin manager.
type CapabilityBuilder struct {
	currentCapabilities Capabilities // schema?
	handlers            []Handler    // now I can gather all of these and the user just has to call `GetHandlers()` and that's it.
	scheme              *runtime.Scheme
}

// NewCapabilityBuilder constructs a new builder for registering capabilities for the given plugin type.
func NewCapabilityBuilder(scheme *runtime.Scheme) *CapabilityBuilder {
	return &CapabilityBuilder{
		currentCapabilities: Capabilities{
			Capabilities: map[PluginType][]Capability{},
		},
		scheme: scheme,
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

// RegisterReadComponentVersionRepositoryCapability defines Transfer type plugin capability of reading or writing
// to an OCI repository. Calling this will automatically register the plugin for the type Transfer.
func RegisterCapability[T runtime.Typed](
	c *CapabilityBuilder,
	proto T,
	handler PluginBase,
) error {

	// look up the registered type from the Type passed to the registration.
	typ, err := c.scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	switch t := handler.(type) {
	case ReadOCMRepositoryPluginContract[T]:
		t.GetComponentVersion()
	case WriteOCMRepositoryPluginContract[T]:
	}

	// Setup capabilities
	c.currentCapabilities.Capabilities[ComponentVersionRepositoryPlugin] = append(c.currentCapabilities.Capabilities[ComponentVersionRepositoryPlugin], Capability{
		Capability: ReadComponentVersionRepositoryCapability,
		Type:       typ,
	})

	// Setup handlers
	c.handlers = append(c.handlers,
		Handler{
			Handler:  DownloadComponentVersionHandlerFunc[T](handlers.GetComponentVersion, c.scheme, proto),
			Location: DownloadComponentVersion,
		},
		Handler{
			Handler:  GetResourceHandlerFunc(handlers.DownloadResource),
			Location: DownloadLocalResource,
		})

	return nil
}

func DownloadComponentVersionHandlerFunc(f GetComponentVersionFn, scheme *runtime.Scheme, typ runtime.Typed) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		// Just put this shit into the SDK since it's type agnostic.
		// It's once per contract.
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

		// Verify that the given scheme from the capability builder contained the runtime Raw type of the repository
		// provided by the implementation.
		// TODO: Make sure the scheme is NOT set to allow unknown.
		if err := scheme.Decode(strings.NewReader(request.Header.Get(XOCMRepositoryHeader)), typ); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), name, version, typ, credentials, writer); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func UploadComponentVersionHandlerFunc[T runtime.Typed](f PostComponentVersionFn[T], scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		req, err := DecodeJSONRequestBody[PostComponentVersionRequest[T]](writer, request)
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

		if err := scheme.Convert(req.Repository, typ); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), req.Descriptor, typ, credentials); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

func GetResourceHandlerFunc(f GetResourceFn) http.HandlerFunc {
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
