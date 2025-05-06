// Package manager provides functionality for discovering, registering, and managing plugins.
// It supports registering plugins from a file system, loading them into appropriate registries, and managing their lifecycle.
// Precise life-cycle management is handled by the plugin client SDK. It has code that deals with start-up, shut-down etc.
// The manager is used for discovery and keeping track of plugins.
//
// The Plugin Manager facilitates the use of plugins by:
//   - Discovering plugins in a given location.
//   - Registering component version repositories.
//
// Plugin Management flow:
//
//	                  PLUGIN MANAGEMENT FLOW
//	                  ----------------------
//	                          |
//	                          v
//	+----------------------------------------------+
//	|  Manager discovers plugins on disk in folder |
//	+----------------------------------------------+
//	                          |
//	                          v
//	+----------------------------------------------+
//	|  For each discovered plugin, call            |
//	|  `capabilities` command that returns         |
//	|   JSON data via stdout                       |
//	+----------------------------------------------+
//	                          |
//	                          v
//	+----------------------------------------------+
//	|  Manager receives plugin's stderr stream     |
//	|  for ongoing log message forwarding          |
//	+----------------------------------------------+
//	                          |
//	                          v
//	+----------------------------------------------+
//	|  Manager stores plugin info (unstarted)      |
//	+----------------------------------------------+
//	                           |
//	                           |
//	+--------------------------+-----------------------------------------------+
//	|                                                                          |
//	v                                                                          v
//
// +---------------------------------------------+    +---------------------------------------------+
// | Elsewhere: Client requests plugin for       |    | GetReadWriteComponentVersionRepository...   |
// | specific functionality using the right      |<-->| attempts to find suitable plugin            |
// | registry                                    |    +---------------------------------------------+
// +---------------------------------------------+
//
//	|
//	v
//
// +---------------------------------------------+    +---------------------------------------------+
// | Function finds matching plugin from registry| -> | Found plugin is started                     |
// +---------------------------------------------+    +---------------------------------------------+
//
//	                                                                           |
//	                                                                           v
//														+----------------------------------------------+
//														| Plugin starts serving configured endpoints   |
//														| - Handles REST calls (GetComponentVersion)   |
//														| - Communicates via JSON payloads over UDS/TCP|
//														+----------------------------------------------+
//
// This shows how to register plugins found at a specified location (directory). The function scans the directory,
// finds plugins, and registers them.
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
// This function will do two things. Find plugins, then figure out what type of plugins they are. The type of the plugin
// is determined through the capability it provides. For example, an OCMComponentVersionRepository type plugin which
// can discover component versions in given repository types ( i.e.: OCI Registry ) has a certain set of endpoints that
// identify it ( i.e.: AddComponentVersion, GetComponentVersion... ).
// This happens through an initial call to `capabilities` command on the plugin which will return a list of types and
// the type of the plugin constructed by one of the above Register* functions that can be used. This process is described
// in the `endpoints` package documentation.
//
// Once the registration is successful, in order to get a plugin, call the appropriate function that gets back the
// right plugin. For example, for OCMComponentVersionRepository plugins the `Get*` function would be:
// `GetReadWriteComponentVersionRepositoryPluginForType`. Usage:
//
//	repo, err := componentversionrepository.GetReadWriteComponentVersionRepositoryPluginForType(ctx, registry, &spec, scheme)
//	r.NoError(err)
//
//	user, pass := "test", "password"
//
//	request := repov1.GetComponentVersionRequest[*v1.OCIRepository]{
//		Repository: &spec,
//		Name:       "ocm.software/ocmcli",
//		Version:    "0.22.1",
//	}
//
//	desc, err := repo.GetComponentVersion(ctx, request, map[string]string{
//		"username": user,
//		"password": pass,
//	})
package manager
