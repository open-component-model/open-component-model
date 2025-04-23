// Package sdk is a lightweight package creating plugins compatible with the plugin manager.
// It contains several useful features to build a Go based plugin that is accepted by the manager.
// The SDK handles the communication protocol, lifecycle management, and health checkin.
// Key concepts in this package:
//   - HTTP-based communication over TCP or Unix sockets (Unix sockets preferred)
//   - Automatic idle timeout management
//   - Graceful shutdown handling
//   - Health checking endpoints
//   - Custom handler registration
//   - Custom cleanup functionality
//
// This package is expected to be used together with one of the plugin registries and the endpoint builder package.
// Those packages provide all the necessary setup code to construct the right handlers. Further, during RegisterHandlers
// call, this package will set up idle timeout management.
//
// GracefulShutdown will handle interrupts and will clean up any created UDSs.
// The following code is an example on how to use this package:
// First, call the appropriate endpoint builder to get the right handlers and config that needs to be sent back to
// the manager:
//
//	scheme := runtime.NewScheme()
//	repository.MustAddToScheme(scheme)
//	capabilities := endpoints.NewEndpoints(scheme)
//
//	if err := componentversionrepository.RegisterComponentVersionRepository(&v1.OCIRepository{}, &OCIPlugin{}, capabilities); err != nil {
//		log.Fatal(err)
//	}
//
// Next, create the plugin. An expected `--config` option should be set up in which the plugin receives further
// configuration available only at startup.
//
//	// Parse command-line arguments
//	configData := flag.String("config", "", "Plugin config.")
//	flag.Parse()
//	if configData == nil || *configData == "" {
//		log.Fatal("Missing required flag --config")
//	}
//
//	conf := types.Config{}
//	if err := json.Unmarshal([]byte(*configData), &conf); err != nil {
//		log.Fatal(err)
//	}
//
//	if conf.ID == "" {
//		log.Fatal("Plugin ID is required.")
//	}
//	if conf.Location == "" {
//		log.Fatal("Plugin location is required.")
//	}
//	r := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
//
//	ocmPlugin := plugin.NewPlugin(r, conf)
//	if err := ocmPlugin.RegisterHandlers(capabilities.GetHandlers()...); err != nil {
//		log.Fatal(err)
//	}
//
//	if err := ocmPlugin.Start(context.Background()); err != nil {
//		log.Fatal(err)
//	}
//
// Once the plugin is started, everything is taken care off by this package.
package sdk
