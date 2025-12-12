package componentversionrepository

import (
	"fmt"

	ocmrepositoryv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
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
	handler ocmrepositoryv1.ReadWriteOCMRepositoryPluginContract[T],
	c *endpoints.EndpointBuilder,
) error {
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
			Handler:  ListComponentVersionsHandlerFunc(handler.ListComponentVersions, c.Scheme, proto),
			Location: ListComponentVersions,
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
		},
		endpoints.Handler{
			Handler:  GetIdentityHandlerFunc(handler.GetIdentity, c.Scheme, proto),
			Location: Identity,
		},
		endpoints.Handler{
			Handler:  AddLocalSourceHandlerFunc(handler.AddLocalSource, c.Scheme),
			Location: UploadLocalSource,
		},
		endpoints.Handler{
			Handler:  GetLocalSourceHandlerFunc(handler.GetLocalSource, c.Scheme, proto),
			Location: DownloadLocalSource,
		},
	)

	schema, err := plugins.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	c.PluginSpec.CapabilitySpecs = append(c.PluginSpec.CapabilitySpecs, &ocmrepositoryv1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(ocmrepositoryv1.ComponentVersionRepositoryPluginType)),
		SupportedRepositorySpecTypes: []types.Type{
			{
				Type:       typ,
				Aliases:    nil,
				JSONSchema: schema,
			},
		},
	})

	return nil
}
