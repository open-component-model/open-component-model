package blobtransformer

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
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
	handler v1.BlobTransformerPluginContract[T],
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
			Handler:  TransformBlobHandlerFunc[T](handler.TransformBlob),
			Location: TransformBlob,
		},
		endpoints.Handler{
			Handler:  GetIdentityHandlerFunc(handler.GetIdentity, c.Scheme, proto),
			Location: Identity,
		},
	)

	schema, err := runtime.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	c.CurrentTypes.Types[types.BlobTransformerPluginType] = append(c.CurrentTypes.Types[types.BlobTransformerPluginType],
		// we only need ONE type because we have multiple endpoints, but those endpoints
		// support the same type with the same schema... Figure out how to differentiate
		// if there are multiple schemas and multiple types so which belongs to which?
		// Maybe it's enough to have a convention where the first typee is the FROM and
		// the second type is the TO part when we construct the type affiliation to the
		// implementation.
		types.Type{
			Type:       typ,
			JSONSchema: schema,
		})

	return nil
}
