package digestprocessor

import (
	"fmt"

	digestprocessorv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ProcessResourceDigest processes the digest of a resource.
	ProcessResourceDigest = "/resource/digest/process"
	// Identity provides the identity of a type supported by the plugin.
	Identity = "/identity"
)

// RegisterDigestProcessor takes a builder and a handler and based on the handler's contract type
// will construct a list of endpoint handlers that they will need. Once completed, MarshalJSON can be
// used to construct the supported endpoint list to give back to the plugin manager. This information is stored
// about the plugin and then used for later lookup. The type is also saved with the endpoint, meaning
// during lookup the right endpoint + type is used.
func RegisterDigestProcessor[T runtime.Typed](
	proto T,
	handler digestprocessorv1.ResourceDigestProcessorContract,
	c *endpoints.EndpointBuilder,
) error {
	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	c.Handlers = append(c.Handlers, endpoints.Handler{
		Handler:  ResourceDigestProcessorHandlerFunc(handler.ProcessResourceDigest),
		Location: ProcessResourceDigest,
	}, endpoints.Handler{
		Handler:  IdentityProcessorHandlerFunc(handler.GetIdentity),
		Location: Identity,
	})

	schema, err := plugins.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	c.PluginSpec.CapabilitySpecs = append(c.PluginSpec.CapabilitySpecs, &digestprocessorv1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(digestprocessorv1.DigestProcessorPluginType)),
		SupportedAccessTypes: []types.Type{
			{
				Type:       typ,
				Aliases:    nil,
				JSONSchema: schema,
			},
		},
	})

	return nil
}
