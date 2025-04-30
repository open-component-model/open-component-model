// Package manager provides functionality for discovering, registering, and managing plugins.
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
//	    err := pm.RegisterPlugins(ctx, "/path/to/plugins")
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
