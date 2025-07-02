package credentialrepository

import (
	"fmt"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RegisterCredentialRepository takes a builder and a handler and based on the handler's contract type
// will construct a list of endpoint handlers that they will need. Once completed, MarshalJSON can be
// used to construct the supported endpoint list to give back to the plugin manager. This information is stored
// about the plugin and then used for later lookup. The type is also saved with the endpoint, meaning
// during lookup the right endpoint + type is used.
func RegisterCredentialRepository[T runtime.Typed](
	proto T,
	handler v1.CredentialRepositoryPluginContract[T],
	c *endpoints.EndpointBuilder,
) error {
	if c.CurrentTypes.Types == nil {
		c.CurrentTypes.Types = map[types.PluginType][]types.Type{}
	}

	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	// Setup handlers for CredentialRepository.
	c.Handlers = append(c.Handlers,
		endpoints.Handler{
			Handler:  ConsumerIdentityForConfigHandlerFunc(handler.ConsumerIdentityForConfig, c.Scheme, proto),
			Location: ConsumerIdentityForConfig,
		},
		endpoints.Handler{
			Handler:  ResolveHandlerFunc(handler.Resolve, c.Scheme, proto),
			Location: Resolve,
		},
	)

	schema, err := runtime.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	c.CurrentTypes.Types[types.CredentialRepositoryPluginType] = append(c.CurrentTypes.Types[types.CredentialRepositoryPluginType],
		types.Type{
			Type:       typ,
			JSONSchema: schema,
		})

	return nil
}
