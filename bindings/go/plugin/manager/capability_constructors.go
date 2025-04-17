package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/invopop/jsonschema"
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

// RegisterCapability takes a plugin capability contract implementation and wraps them into an appropriate
// http handler. Constructs a capability matrix from the type provided to the Register method and
// determines the correct endpoints. Once the capabilities are built up, call PrintCapabilities to return
// them to the plugin manager.
func RegisterCapability[T runtime.Typed](
	c *CapabilityBuilder,
	proto T,
	handler PluginBase,
) error {

	typ, err := c.scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	switch t := handler.(type) {
	case ReadOCMRepositoryPluginContract[T]:
		// Setup handlers
		c.handlers = append(c.handlers,
			Handler{
				Handler:  GetComponentVersionHandlerFunc(t.GetComponentVersion, c.scheme, proto),
				Location: DownloadComponentVersion,
			},
			Handler{
				Handler:  GetLocalResourceHandlerFunc(t.GetLocalResource, c.scheme, proto),
				Location: DownloadLocalResource,
			})

		schemaOCIRegistry, err := jsonschema.Reflect(proto).MarshalJSON()
		if err != nil {
			return err
		}

		c.currentCapabilities.Capabilities[ComponentVersionRepositoryPlugin] = append(c.currentCapabilities.Capabilities[ComponentVersionRepositoryPlugin], Capability{
			Name: ReadComponentVersionRepositoryCapability,
			Endpoints: []Endpoint{
				{
					Location: DownloadComponentVersion,
					Types: []Type{
						{
							Type:       typ,
							JSONSchema: schemaOCIRegistry,
						},
					},
				},
				{
					Location: DownloadLocalResource,
					Types: []Type{
						{
							Type:       typ,
							JSONSchema: schemaOCIRegistry,
						},
					},
				},
			},
		})
	case WriteOCMRepositoryPluginContract[T]:
	}

	// Setup capabilities
	//c.currentCapabilities.Capabilities[ComponentVersionRepositoryPlugin] = append(c.currentCapabilities.Capabilities[ComponentVersionRepositoryPlugin], Capability{
	//	Capability: ReadComponentVersionRepositoryCapability,
	//	Type:       typ,
	//})

	return nil
}

func GetComponentVersionHandlerFunc[T runtime.Typed](f func(ctx context.Context, request GetComponentVersionRequest[T], credentials Attributes) (*descriptor.Descriptor, error), scheme *runtime.Scheme, typ T) http.HandlerFunc {
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

		if err := scheme.Decode(strings.NewReader(request.Header.Get(XOCMRepositoryHeader)), typ); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		desc, err := f(request.Context(), GetComponentVersionRequest[T]{
			Repository: typ,
			Name:       name,
			Version:    version,
		}, credentials)
		if err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		if err := json.NewEncoder(writer).Encode(desc); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

//
//func UploadComponentVersionHandlerFunc[T runtime.Typed](f PostComponentVersionFn[T], scheme *runtime.Scheme, typ T) http.HandlerFunc {
//	return func(writer http.ResponseWriter, request *http.Request) {
//		req, err := DecodeJSONRequestBody[PostComponentVersionRequest[T]](writer, request)
//		if err != nil {
//			NewError(err, http.StatusBadRequest).Write(writer)
//			return
//		}
//		rawCredentials := []byte(request.Header.Get("Authorization"))
//		credentials := Attributes{}
//		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
//			NewError(err, http.StatusBadRequest).Write(writer)
//			return
//		}
//
//		if err := scheme.Convert(req.Repository, typ); err != nil {
//			NewError(err, http.StatusBadRequest).Write(writer)
//			return
//		}
//
//		if err := f(request.Context(), req.Descriptor, typ, credentials); err != nil {
//			NewError(err, http.StatusInternalServerError).Write(writer)
//			return
//		}
//	}
//}

func GetLocalResourceHandlerFunc[T runtime.Typed](f func(ctx context.Context, request GetLocalResourceRequest[T], credentials Attributes) error, scheme *runtime.Scheme, typ T) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		name := query.Get("name")
		version := query.Get("version")
		targetLocation := Location{
			LocationType: LocationType(query.Get("target_location_type")),
			Value:        query.Get("target_location_value"),
		}
		identityQuery := query.Get("identity")
		decodedIdentity, err := base64.StdEncoding.DecodeString(identityQuery)
		if err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}

		identity := map[string]string{}
		if identityQuery != "" {
			if err := json.Unmarshal(decodedIdentity, &identity); err != nil {
				NewError(err, http.StatusBadRequest).Write(writer)
				return
			}
		}

		if err := scheme.Decode(strings.NewReader(request.Header.Get(XOCMRepositoryHeader)), typ); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		rawCredentials := []byte(request.Header.Get("Authorization"))
		credentials := Attributes{}
		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
			NewError(err, http.StatusBadRequest).Write(writer)
			return
		}

		if err := f(request.Context(), GetLocalResourceRequest[T]{
			Repository:     typ,
			Name:           name,
			Version:        version,
			Identity:       identity,
			TargetLocation: targetLocation,
		}, credentials); err != nil {
			NewError(err, http.StatusInternalServerError).Write(writer)
			return
		}
	}
}

//
//func PostResourceHandlerFunc(f PostResourceFn, schema []byte) http.HandlerFunc {
//	return func(writer http.ResponseWriter, request *http.Request) {
//		body, err := DecodeJSONRequestBody[PostResourceRequest](writer, request)
//		if err != nil {
//			NewError(err, http.StatusInternalServerError).Write(writer)
//			return
//		}
//
//		rawCredentials := []byte(request.Header.Get("Authorization"))
//		credentials := Attributes{} // TODO: Change these to Attributes
//		if err := json.Unmarshal(rawCredentials, &credentials); err != nil {
//			NewError(err, http.StatusBadRequest).Write(writer)
//			return
//		}
//
//		if err := f(request.Context(), body, credentials, writer); err != nil {
//			NewError(err, http.StatusInternalServerError).Write(writer)
//		}
//	}
//}

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
