// Package manager provides the Plugin Manager functionality for discovering, registering, and managing plugins.
// It supports registering plugins from a file system, loading them into appropriate registries, and managing their lifecycle.
//
// The Plugin Manager facilitates the use of plugins by:
//   - Discovering plugins in a given location.
//   - Registering component version repositories.
//   - Handling plugin execution and connection management.
//   - Allowing plugin shutdown and cleanup.
//
// There are two major facilities in this package.
//
// 1. **Endpoints/Capability builder**:
//
// This shows how to register a ComponentVersionRepository plugin. The handler provides the plugin's capability functions.
//
//	type OCIPlugin struct{}
//
//	func (m *OCIPlugin) Ping(_ context.Context) error {
//		return nil
//	}
//
//	func (m *OCIPlugin) GetComponentVersion(ctx context.Context, request types.GetComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Descriptor, error) {
//		_, _ = fmt.Fprintf(os.Stdout, "Returning a descriptor: %+v\n", request.Name)
//		return nil, nil
//	}
//
//	func (m *OCIPlugin) GetLocalResource(ctx context.Context, request types.GetLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
//		_, _ = fmt.Fprintf(os.Stdout, "Writing my local resource here to target: %+v\n", request.TargetLocation)
//		return nil
//	}
//
//	func (m *OCIPlugin) AddLocalResource(ctx context.Context, request types.PostLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Resource, error) {
//		_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource: %+v\n", request.ResourceLocation)
//		return nil, nil
//	}
//
//	func (m *OCIPlugin) AddComponentVersion(ctx context.Context, request types.PostComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
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
// functionality. Meaning, it can be used to download/upload component versions and local resources.
//
// A single binary can declare multiple of these functionalities but never multiple TYPES for the same functionality.
// For example, it could provide being a credential provider of type OCI and a ComponentVersion repository of type OCI.
//
// 2. **Plugin registration and lifecycle management**:
//
// This shows how to register plugins found at a specified location (directory). The function scans the directory, finds plugins, and registers them.
//
//	package main
//
//	import (
//	    "context"
//	    "fmt"
//
//	    "example.com/manager"
//	)
//
//	func main() {
//	    ctx := context.Background()
//	    logger := slog.New(slog.NewTextHandler(os.Stdout))
//	    pm := manager.NewPluginManager(ctx, logger)
//
//	    err := pm.RegisterPluginsAtLocation(ctx, "/path/to/plugins")
//	    if err != nil {
//	        fmt.Println("Error registering plugins:", err)
//	    }
//	}
//
// This function will do two things. Find plugins, then figure out what KIND of plugins they are. It does that by
// convention. It calls a `capabilities` endpoint which will return a list of types and the type of the plugin constructed
// by one of the above Register* functions that can be used. These functions, internally, will construct a list of
// wrappers and endpoints that the plugin then can use to offer functionality.
package manager
