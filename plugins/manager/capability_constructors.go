package manager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	schemaconverter "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v6"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Handler contains the handling function, the location and the Schema for wrapping function calls with.
// For example, a real function passed in from the plugin is wrapper into an HTTP HandlerFunc to be called
// later. This handler will use the Schema in order to verify any access spec information that is passed in.
type Handler struct {
	Handler  http.HandlerFunc
	Location string
	Schema   []byte
}

// GetComponentVersionFn these functions provide the structure that plugins need to implement.
type (
	GetComponentVersionFn  func(ctx context.Context, name, version string, registry runtime.Typed, credentials Attributes, writer io.Writer) (err error)
	PostComponentVersionFn func(ctx context.Context, descriptor *descriptor.Descriptor, registry runtime.Typed, credentials Attributes) error
	GetResourceFn          func(ctx context.Context, request *GetResourceRequest, credentials Attributes, writer io.Writer) error
	PostResourceFn         func(ctx context.Context, request *PostResourceRequest, credentials Attributes, writer io.Writer) error
)

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
func NewCapabilityBuilder() *CapabilityBuilder {
	return &CapabilityBuilder{
		currentCapabilities: Capabilities{
			Capabilities: map[PluginType][]Capability{},
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

// RegisterReadWriteComponentVersionRepositoryCapability defines Transfer type plugin capability of reading or writing
// to an OCI repository. Calling this will automatically register the plugin for the type Transfer.
func (c *CapabilityBuilder) RegisterReadWriteComponentVersionRepositoryCapability(
	typ runtime.Typed,
	handlers ReadWriteComponentVersionRepositoryHandlersOpts,
) error {
	// Schema is derived from the passed in type and matched in the Rest Wrapper.
	// The schema here is the setup from the plugin. The REST endpoint's match
	// will come from the actual passed in Access Spec converted to a type.
	schemaOCIRegistry, err := schemaconverter.Reflect(typ).MarshalJSON()
	if err != nil {
		return err
	}

	// Setup capabilities
	c.currentCapabilities.Capabilities[TransferPlugin] = append(c.currentCapabilities.Capabilities[TransferPlugin], Capability{
		Capability: ReadWriteComponentVersionRepositoryCapability,
		Type:       typ.GetType().String(),
	})

	// Setup handlers
	c.handlers = append(c.handlers, Handler{
		Handler:  UploadComponentVersionHandlerFunc(handlers.UploadComponentVersion, typ),
		Location: UploadComponentVersion, // These should be coming from somewhere because we need to call them later.
	},
		Handler{
			Handler:  DownloadComponentVersionHandlerFunc(handlers.GetComponentVersion, typ),
			Location: DownloadComponentVersion,
		},
		Handler{
			Handler:  PostResourceHandlerFunc(handlers.UploadResource, schemaOCIRegistry),
			Location: UploadLocalResource,
			Schema:   schemaOCIRegistry,
		},
		Handler{
			Handler:  GetResourceHandlerFunc(handlers.DownloadResource, schemaOCIRegistry),
			Location: DownloadLocalResource,
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

		if schema != nil {
			// TODO: Figure out the type.
			ok, err := validatePlugin(nil, schema)
			if err != nil {
				NewError(err, http.StatusBadRequest).Write(writer)
				return
			}

			if !ok {
				NewError(nil, http.StatusBadRequest).Write(writer)
				return
			}
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

// only run validation if a schema exists.
func validatePlugin(typ runtime.Typed, schema []byte) (bool, error) {
	c := jsonschema.NewCompiler()
	unmarshaler, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return false, err
	}

	var v any
	if err := json.Unmarshal(schema, &v); err != nil {
		return true, err
	}

	if err := c.AddResource("schema.json", unmarshaler); err != nil {
		return true, fmt.Errorf("failed to add schema.json: %w", err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return true, fmt.Errorf("failed to compile schema.json: %w", err)
	}

	// need to marshal the interface into a JSON format.
	content, err := json.Marshal(typ)
	if err != nil {
		return true, fmt.Errorf("failed to marshal type: %w", err)
	}
	// once marshalled, we create a map[string]any representation of the marshaled content.
	unmarshalledType, err := jsonschema.UnmarshalJSON(bytes.NewReader(content))
	if err != nil {
		return true, fmt.Errorf("failed to unmarshal : %w", err)
	}

	if _, ok := unmarshalledType.(string); ok {
		// TODO: In _not_ POC this should be either a type switch, or some kind of exclusion or we should change how
		// we register and look up plugins to avoid validating when listing or for certain plugins.
		// skip validation if the passed in type is of type string.
		return true, nil
	}

	// finally, validate map[string]any against the loaded schema
	if err := sch.Validate(unmarshalledType); err != nil {
		var typRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&typRaw, content, "", "  "))
		var schemaRaw bytes.Buffer
		err = errors.Join(err, json.Indent(&schemaRaw, schema, "", "  "))
		return true, fmt.Errorf("failed to validate schema for\n%s\n---SCHEMA---\n%s\n: %w", typRaw.String(), schemaRaw.String(), err)
	}

	return true, nil
}
