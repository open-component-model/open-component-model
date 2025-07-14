// Package sdk is a package providing an SDK compatible with the plugin manager for writing your own OCM plugins in go.
// It helps to manage a plugin's life-cycle and general behavior needs. These are the following:
//   - starting a plugin
//   - graceful shutdown of a plugin
//   - registering handlers
//   - idle check ( after a configured amount of times being without a task will automatically shut down the plugin to prevent resource usage )
//   - determine listening address ( for tcp: get a free port and listen on it; unix: create a name of the socket )
//
// GracefulShutdown will handle interrupts and will clean up any created unix domain sockets if any were created.
// The following code is an example on how to use this package:
// First, call the appropriate endpoint builder to get the right handlers and config that needs to be sent back to
// the manager:
//
//	scheme := runtime.NewScheme()
//	repository.MustAddToScheme(scheme)
//	capabilities := endpoints.NewEndpoints(scheme)
//
//	if err := componentrepository.RegisterComponentVersionRepository(&v1.OCIRepository{}, &OCIPlugin{}, capabilities); err != nil {
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
//
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
