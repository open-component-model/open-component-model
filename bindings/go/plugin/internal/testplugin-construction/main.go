package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/construction/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	constructorrepositroy "ocm.software/open-component-model/bindings/go/plugin/manager/registries/constructorrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TestPlugin struct{}

var logger *slog.Logger

func (m *TestPlugin) GetIdentity(ctx context.Context, typ v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	_, _ = fmt.Fprintf(os.Stdout, "GetIdentity: %+v\n", typ.Typ)
	return runtime.Identity{}, nil
}

func (m *TestPlugin) ProcessResource(ctx context.Context, request v1.ProcessResourceRequest, credentials map[string]string) (v1.ProcessResourceResponse, error) {
	return v1.ProcessResourceResponse{
		Resource: &constructorv1.Resource{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    "test-resource",
					Version: "v0.0.1",
				},
			},
			Type:     "type",
			Relation: "local",
		},
		Location: &types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        "/tmp/to/file",
		},
	}, nil
}

func (m *TestPlugin) Ping(_ context.Context) error {
	return nil
}

var _ v1.ResourceInputPluginContract = &TestPlugin{}

func main() {
	args := os.Args[1:]
	// log messages are shared over stderr by convention established by the plugin manager.
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug, // debug level here is respected when sending this message.
	}))

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	if err := constructorrepositroy.RegisterResourceInputProcessor(&dummyv1.Repository{}, &TestPlugin{}, capabilities); err != nil {
		logger.Error("failed to register test plugin", "error", err.Error())
		os.Exit(1)
	}

	logger.Info("registered test plugin")

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := json.Marshal(capabilities)
		if err != nil {
			logger.Error("failed to marshal capabilities", "error", err)
			os.Exit(1)
		}

		if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
			logger.Error("failed print capabilities", "error", err)
			os.Exit(1)
		}

		logger.Info("capabilities sent")

		os.Exit(0)
	}

	// Parse command-line arguments
	configData := flag.String("config", "", "Plugin config.")
	flag.Parse()
	if configData == nil || *configData == "" {
		logger.Error("missing required flag --config")
		os.Exit(1)
	}

	conf := types.Config{}
	if err := json.Unmarshal([]byte(*configData), &conf); err != nil {
		logger.Error("failed to unmarshal config", "error", err)
		os.Exit(1)
	}
	logger.Debug("config data", "config", conf)

	if conf.ID == "" {
		logger.Error("plugin config has no ID")
		os.Exit(1)
	}

	separateContext := context.Background()
	ocmPlugin := plugin.NewPlugin(separateContext, logger, conf, os.Stdout)
	if err := ocmPlugin.RegisterHandlers(capabilities.GetHandlers()...); err != nil {
		logger.Error("failed to register handlers", "error", err)
		os.Exit(1)
	}

	logger.Info("starting up plugin", "plugin", conf.ID)

	if err := ocmPlugin.Start(context.Background()); err != nil {
		logger.Error("failed to start plugin", "error", err)
		os.Exit(1)
	}
}
