package constructorrepositroy

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/construction/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ProcessResource processes the input of a resource.
	ProcessResource = "/resource/process"
	// ProcessSource processes the input of a source.
	ProcessSource = "/source/process"
	// Identity provides the identity of a type supported by the plugin.
	Identity = "/identity"
	// ProcessResourceDigest processes the digest of a resource.
	ProcessResourceDigest = "/resource/process/digest"
)

// RegisterResourceInputProcessor takes a builder and a handler and based on the handler's contract type
// will construct a list of endpoint handlers that they will need. Once completed, MarshalJSON can be
// used to construct the supported endpoint list to give back to the plugin manager. This information is stored
// about the plugin and then used for later lookup. The type is also saved with the endpoint, meaning
// during lookup the right endpoint + type is used.
func RegisterResourceInputProcessor[T runtime.Typed](
	proto T,
	handler v1.ResourceInputPluginContract,
	c *endpoints.EndpointBuilder,
) error {
	if c.CurrentTypes.Types == nil {
		c.CurrentTypes.Types = map[types.PluginType][]types.Type{}
	}

	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	c.Handlers = append(c.Handlers, endpoints.Handler{
		Handler:  ResourceInputProcessorHandlerFunc(handler.ProcessResource, c.Scheme, proto),
		Location: ProcessResource,
	})

	schema, err := runtime.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	c.CurrentTypes.Types[types.ConstructionResourceInputPluginType] = append(c.CurrentTypes.Types[types.ConstructionResourceInputPluginType],
		types.Type{
			Type:       typ,
			JSONSchema: schema,
		})

	return nil
}
