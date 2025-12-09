package blobtransformer

import (
	"fmt"

	blobtransformerv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RegisterBlobTransformer takes a builder and a handler and based on the handler's contract type
// will construct a list of endpoint handlers that they will need. Once completed, MarshalJSON can be
// used to construct the supported endpoint list to give back to the plugin manager. This information is stored
// about the plugin and then used for later lookup. The type is also saved with the endpoint, meaning
// during lookup the right endpoint + type is used.
func RegisterBlobTransformer[T runtime.Typed](
	proto T,
	handler blobtransformerv1.BlobTransformerPluginContract[T],
	c *endpoints.EndpointBuilder,
) error {
	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	// Setup handlers for ComponentVersionRepository.
	c.Handlers = append(c.Handlers,
		endpoints.Handler{
			Handler:  TransformBlobHandlerFunc[T](handler.TransformBlob),
			Location: TransformBlob,
		},
		endpoints.Handler{
			Handler:  GetIdentityHandlerFunc(handler.GetIdentity, c.Scheme, proto),
			Location: Identity,
		},
	)

	schema, err := plugins.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	c.PluginSpec.CapabilitySpecs = append(c.PluginSpec.CapabilitySpecs, &blobtransformerv1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(blobtransformerv1.BlobTransformerPluginType)),
		SupportedTransformerSpecTypes: []types.Type{
			{
				Type:       typ,
				Aliases:    nil,
				JSONSchema: schema,
			},
		},
	})

	return nil
}
