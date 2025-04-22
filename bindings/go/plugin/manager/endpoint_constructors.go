package manager

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/invopop/jsonschema"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Handler contains the handling function, the location and the Schema for wrapping function calls with.
// For example, a real function passed in from the plugin is wrapper into an HTTP HandlerFunc to be called
// later. This handler will use the Schema in order to verify any access spec information that is passed in.
type Handler struct {
	Handler  http.HandlerFunc
	Location string
}

// EndpointBuilder constructs a capability for the plugin. Register*Endpoint will keep updating
// an internal tracker. Once all capabilities have been declared, we call Marshal to
// return the registered capabilities to the plugin manager.
type EndpointBuilder struct {
	currentTypes types.Types
	handlers     []Handler
	scheme       *runtime.Scheme
}

// NewEndpoints constructs a new builder for registering capabilities for the given plugin type.
func NewEndpoints(scheme *runtime.Scheme) *EndpointBuilder {
	return &EndpointBuilder{
		scheme: scheme,
	}
}

// MarshalJSON returns the accumulated endpoints during Register* calls.
func (c *EndpointBuilder) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.currentTypes)
}

// GetHandlers returns all the handlers that this plugin implemented during the registration of a capability.
func (c *EndpointBuilder) GetHandlers() []Handler {
	return c.handlers
}

// RegisterComponentVersionRepository takes a builder and a handler and based on the handler's contract type
// will construct a list of endpoint handlers that they will need. Once completed, MarshalJSON can be
// used to construct the supported endpoint list to give back to the plugin manager. This information is stored
// about the plugin and then used for later lookup. The type is also saved with the endpoint, meaning
// during lookup the right endpoint + type is used.
func RegisterComponentVersionRepository[T runtime.Typed](
	proto T,
	handler contracts.ReadWriteOCMRepositoryPluginContract[T],
	c *EndpointBuilder,
) error {
	if c.currentTypes.Types == nil {
		c.currentTypes.Types = map[types.PluginType][]types.Type{}
	}

	typ, err := c.scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	// Setup handlers for ComponentVersionRepository.
	c.handlers = append(c.handlers,
		Handler{
			Handler:  componentversionrepository.GetComponentVersionHandlerFunc(handler.GetComponentVersion, c.scheme, proto),
			Location: componentversionrepository.DownloadComponentVersion,
		},
		Handler{
			Handler:  componentversionrepository.GetLocalResourceHandlerFunc(handler.GetLocalResource, c.scheme, proto),
			Location: componentversionrepository.DownloadLocalResource,
		},
		Handler{
			Handler:  componentversionrepository.AddComponentVersionHandlerFunc(handler.AddComponentVersion),
			Location: componentversionrepository.UploadComponentVersion,
		},
		Handler{
			Handler:  componentversionrepository.AddLocalResourceHandlerFunc(handler.AddLocalResource),
			Location: componentversionrepository.UploadLocalResource,
		})
	schemaOCIRegistry, err := jsonschema.Reflect(proto).MarshalJSON()
	if err != nil {
		return err
	}

	c.currentTypes.Types[types.ComponentVersionRepositoryPluginType] = append(c.currentTypes.Types[types.ComponentVersionRepositoryPluginType],
		// we only need ONE type because we have multiple endpoints, but those endpoints
		// support the same type with the same schema... Figure out how to differentiate
		// if there are multiple schemas and multiple types so which belongs to which?
		// Maybe it's enough to have a convention where the first typee is the FROM and
		// the second type is the TO part when we construct the type affiliation to the
		// implementation.
		types.Type{
			Type:       typ,
			JSONSchema: schemaOCIRegistry,
		})

	return nil
}
