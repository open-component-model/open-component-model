package componentversionrepository

import (
	"fmt"

	"github.com/invopop/jsonschema"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RegisterComponentVersionRepository takes a builder and a handler and based on the handler's contract type
// will construct a list of endpoint handlers that they will need. Once completed, MarshalJSON can be
// used to construct the supported endpoint list to give back to the plugin manager. This information is stored
// about the plugin and then used for later lookup. The type is also saved with the endpoint, meaning
// during lookup the right endpoint + type is used.
func RegisterComponentVersionRepository[T runtime.Typed](
	proto T,
	handler contracts.ReadWriteOCMRepositoryPluginContract[T],
	c *endpoints.EndpointBuilder,
) error {
	if c.CurrentTypes.Types == nil {
		c.CurrentTypes.Types = map[types.PluginType][]types.Type{}
	}

	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	// Setup handlers for ComponentVersionRepository.
	c.Handlers = append(c.Handlers,
		endpoints.Handler{
			Handler:  GetComponentVersionHandlerFunc(handler.GetComponentVersion, c.Scheme, proto),
			Location: DownloadComponentVersion,
		},
		endpoints.Handler{
			Handler:  GetLocalResourceHandlerFunc(handler.GetLocalResource, c.Scheme, proto),
			Location: DownloadLocalResource,
		},
		endpoints.Handler{
			Handler:  AddComponentVersionHandlerFunc(handler.AddComponentVersion),
			Location: UploadComponentVersion,
		},
		endpoints.Handler{
			Handler:  AddLocalResourceHandlerFunc(handler.AddLocalResource, c.Scheme),
			Location: UploadLocalResource,
		})
	schemaOCIRegistry, err := jsonschema.Reflect(proto).MarshalJSON()
	if err != nil {
		return err
	}

	c.CurrentTypes.Types[types.ComponentVersionRepositoryPluginType] = append(c.CurrentTypes.Types[types.ComponentVersionRepositoryPluginType],
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
