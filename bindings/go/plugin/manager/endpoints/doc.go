// Package endpoints shows how to register a ComponentVersionRepository plugin. The handler provides the plugin's capability functions.
//
//	type OCIPlugin struct{}
//
//	func (m *OCIPlugin) Ping(_ context.Context) error {
//		return nil
//	}
//
//	func (m *OCIPlugin) GetComponentVersion(ctx context.Context, request types.GetComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Descriptor, error) {
//		_, _ = fmt.Fprintf(os.Stdout, "Returning a descriptor: %+v\n", request.Name)
//		return nil, nil
//	}
//
//	func (m *OCIPlugin) GetLocalResource(ctx context.Context, request types.GetLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) error {
//		_, _ = fmt.Fprintf(os.Stdout, "Writing my local resource here to target: %+v\n", request.TargetLocation)
//		return nil
//	}
//
//	func (m *OCIPlugin) AddLocalResource(ctx context.Context, request types.PostLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Resource, error) {
//		_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource: %+v\n", request.ResourceLocation)
//		return nil, nil
//	}
//
//	func (m *OCIPlugin) AddComponentVersion(ctx context.Context, request types.PostComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) error {
//		_, _ = fmt.Fprintf(os.Stdout, "AddComponentVersion: %+v\n", request.Descriptor.Component.Name)
//		return nil
//	}
//
//	var _ contracts.ReadWriteOCMRepositoryPluginContract[*v1.OCIRepository] = &OCIPlugin{}
//	func main() {
//		args := os.Args[1:]
//		scheme := runtime.NewScheme()
//		repository.MustAddToScheme(scheme)
//		capabilities := manager.NewEndpoints(scheme)
//
//		if err := manager.RegisterComponentVersionRepository(&v1.OCIRepository{}, &OCIPlugin{}, capabilities); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// In order to declare that a plugin supports a certain set of functionalities it will have to implement a given set of
// functions. These functions have set requests and return types that the plugin will need to provide. The interfaces are
// tightly coupled to these functionalities. For example, the above plugin implements the OCMComponentVersionRepository
// contract. Meaning, it can be used to download/upload component versions and local resources.
//
// A single binary can declare multiple of these functionalities but never multiple TYPES for the same functionality.
// For example, it could provide being a credential provider of type OCI and a ComponentVersion repository of type OCI.
package endpoints
