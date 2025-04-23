# OCM Plugin SDK

A lightweight Go SDK for creating plugins compatible with the Open Component Model (OCM) plugin manager.

## Overview

The OCM Plugin SDK provides a framework for creating Go-based plugins that integrate with the OCM plugin manager.
This SDK handles the communication protocol, lifecycle management, and health checking between your plugin and the OCM system.

Key features:
- HTTP-based communication over TCP or Unix sockets (Unix sockets preferred)
- Automatic idle timeout management
- Graceful shutdown handling
- Health checking endpoints
- Custom handler registration
- Cleanup functionality

## Quick Start

Here's a simple example of creating a plugin:

```go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type OCIPlugin struct{}

func (m *OCIPlugin) Ping(_ context.Context) error {
	return nil
}

func (m *OCIPlugin) GetComponentVersion(ctx context.Context, request types.GetComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Descriptor, error) {
	_, _ = fmt.Fprintf(os.Stdout, "Returning a descriptor: %+v\n", request.Name)
	return nil, nil
}

func (m *OCIPlugin) GetLocalResource(ctx context.Context, request types.GetLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "Writing my local resource here to target: %+v\n", request.TargetLocation)
	return nil
}

func (m *OCIPlugin) AddLocalResource(ctx context.Context, request types.PostLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Resource, error) {
	_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource: %+v\n", request.ResourceLocation)
	return nil, nil
}

func (m *OCIPlugin) AddComponentVersion(ctx context.Context, request types.PostComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "AddComponentVersiont: %+v\n", request.Descriptor.Component.Name)
	return nil
}

// Make sure we implement the contract we would like this plugin to be serving functionality for.
var _ contracts.ReadWriteOCMRepositoryPluginContract[*v1.OCIRepository] = &OCIPlugin{}

func main() {
	args := os.Args[1:]

	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	capabilities := manager.NewEndpoints(scheme)

	// Register this plugin. Note that `capabilities` will be updated internally. This update then can be used
	// to provide `capabilities` information to the plugin manager by calling json.Marshal on it and writing
	// the result to stdout.
	if err := manager.RegisterComponentVersionRepository(&v1.OCIRepository{}, &OCIPlugin{}, capabilities); err != nil {
		log.Fatal(err)
	}

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := json.Marshal(capabilities)
		if err != nil {
			log.Fatal(err)
		}

		if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
			log.Fatal(err)
		}

		os.Exit(0)
	}

	// Parse command-line arguments
	configData := flag.String("config", "", "Plugin config.")
	flag.Parse()
	if configData == nil || *configData == "" {
		log.Fatal("Missing required flag --config")
	}

	// This configuration contains information for the plugin such as, the unix domain socket location
	// where it needs to listen on; ID and other information.
	conf := types.Config{}
	if err := json.Unmarshal([]byte(*configData), &conf); err != nil {
		log.Fatal(err)
	}

	if conf.ID == "" {
		log.Fatal("Plugin ID is required.")
	}
	if conf.Location == "" {
		log.Fatal("Plugin location is required.")
	}
	r := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

	ocmPlugin := plugin.NewPlugin(r, conf)
	// The `capabilities` also contains the handlers that this plugin needs to declare. The handlers here are
	// wrappers around the above implemented interface methods.
	if err := ocmPlugin.RegisterHandlers(capabilities.GetHandlers()...); err != nil {
		log.Fatal(err)
	}

	if err := ocmPlugin.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}

```

## Plugin Configuration

The plugin configuration is defined using the `types.Config` struct:

```go
config := types.Config{
    ID:          "my-plugin",         // Unique identifier for your plugin
    Type:        types.Socket,        // Communication type (Socket or TCP)
    Location:    "/tmp/my-plugin.sock", // Socket path or TCP address
    IdleTimeout: &idleTimeout,        // Optional: Duration before auto-shutdown when idle
}
```

Communication types:
- `types.Socket`: Unix domain socket
- `types.TCP`: TCP network socket

## Registering Handlers

Register handlers to respond to specific endpoints:

```go
plugin.RegisterHandlers(
    manager.Handler{
        Location: "/api/v1/endpoint",
        Handler: func(w http.ResponseWriter, r *http.Request) {
            // Handler logic
        },
    },
    // More handlers...
)
```

Each handler automatically tracks work state to prevent the plugin from timing out while processing requests.
This function is usually called through the capabilities/endpoints builder helper.

## Built-in Endpoints

The SDK automatically registers the following endpoints:

- `/healthz` - Health check endpoint that returns HTTP 200 OK when the plugin is functioning
- `/shutdown` - Endpoint to trigger graceful shutdown of the plugin

## Idle Management

The plugin monitors idle time and will automatically shut down after the specified `IdleTimeout` duration if no work is
being performed. This helps conserve resources when the plugin isn't actively being used.

The SDK provides methods to indicate when work is being performed:

```go
// Manually indicate work is starting
plugin.StartWork()

// Perform work...

// Indicate work is complete
plugin.StopWork()
```

This is handled automatically for registered handlers.

## Graceful Shutdown

The plugin handles graceful shutdown in response to:
- SIGINT/SIGTERM signals
- Calls to the `/shutdown` endpoint
- Idle timeout expiration

During shutdown:
1. The HTTP server stops accepting new connections
2. Socket files are removed (for Unix socket plugins)
3. The custom cleanup function is called if registered

## Custom Cleanup

Register a custom cleanup function to perform additional tasks during shutdown:

```go
plugin.RegisterCleanupFunc(func(ctx context.Context) error {
    // Custom cleanup logic
    return nil
})
```

The cleanup function receives a context that may have a timeout, so cleanup operations should respect context cancellation.

## Logging

The plugin uses Go's `slog` package for structured logging. Provide your own logger instance when creating the plugin,
or the SDK will create a default text logger to stdout.

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
plugin := sdk.NewPlugin(logger, config)
```
