package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobtransformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TestPlugin struct{}

func (m *TestPlugin) TransformBlob(ctx context.Context, request *v1.TransformBlobRequest[*dummyv1.Repository], credentials map[string]string) (*v1.TransformBlobResponse, error) {
	file, err := os.CreateTemp("", "test-blob-file")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	_, _ = file.Write([]byte("transformed-blob"))
	_ = file.Close()
	logger.Debug("TransformResource", "location", file.Name())
	return &v1.TransformBlobResponse{
		Location: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        file.Name(),
		},
	}, nil
}

var logger *slog.Logger

func (m *TestPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[*dummyv1.Repository]) (*v1.GetIdentityResponse, error) {
	_, _ = fmt.Fprintf(os.Stdout, "GetIdentity: %+v\n", typ.Typ)
	return nil, nil
}

func (m *TestPlugin) Ping(_ context.Context) error {
	return nil
}

var _ v1.BlobTransformerPluginContract[*dummyv1.Repository] = &TestPlugin{}

func main() {
	args := os.Args[1:]
	// log messages are shared over stderr by convention established by the plugin manager.
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug, // debug level here is respected when sending this message.
	}))

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	if err := blobtransformer.RegisterBlobTransformer(&dummyv1.Repository{}, &TestPlugin{}, capabilities); err != nil {
		logger.Error("failed to register test plugin", "error", err.Error())
		os.Exit(1)
	}

	logger.Info("registered test plugin")

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := capabilities.MarshalJSON()
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
