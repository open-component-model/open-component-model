package input

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
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
)

// RegisterInputProcessor takes a builder and a handler and based on the handler's contract type
// will construct a list of endpoint handlers that they will need. Once completed, MarshalJSON can be
// used to construct the supported endpoint list to give back to the plugin manager. This information is stored
// about the plugin and then used for later lookup. The type is also saved with the endpoint, meaning
// during lookup the right endpoint + type is used.
func RegisterInputProcessor[T runtime.Typed](
	proto T,
	handler v1.InputPluginContract,
	c *endpoints.EndpointBuilder,
) error {
	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	c.Handlers = append(c.Handlers, endpoints.Handler{
		Handler:  ResourceInputProcessorHandlerFunc(handler.ProcessResource, c.Scheme, proto),
		Location: ProcessResource,
	}, endpoints.Handler{
		Handler:  SourceInputProcessorHandlerFunc(handler.ProcessSource, c.Scheme, proto),
		Location: ProcessSource,
	})

	schema, err := plugins.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	c.PluginSpec.CapabilitySpecs = append(c.PluginSpec.CapabilitySpecs, &v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.InputPluginType)),
		SupportedInputTypes: []types.Type{
			{
				Type:       typ,
				Aliases:    nil,
				JSONSchema: schema,
			},
		},
	})

	return nil
}
