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
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	listerv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TestPlugin struct {
	contracts.EmptyBasePlugin
}

var _ listerv1.ComponentListerPluginContract[*dummyv1.Repository] = (*TestPlugin)(nil)

func (m *TestPlugin) ListComponents(ctx context.Context, request *listerv1.ListComponentsRequest[*dummyv1.Repository], credentials map[string]string) (*listerv1.ListComponentsResponse, error) {
	logger.DebugContext(ctx, "ListComponents", "request", request)
	page1 := []string{"test-component-1"}
	page2 := []string{"test-component-2"}
	response := &listerv1.ListComponentsResponse{}
	var err error

	switch request.Last {
	case "":
		response.List = page1
		response.Header = &listerv1.ListComponentsResponseHeader{Last: page1[len(page1)-1]}
	case page1[len(page1)-1]:
		response.List = page2
	default:
		err = fmt.Errorf("unknown last: %q", request.Last)
	}

	return response, err
}

func (m *TestPlugin) GetIdentity(ctx context.Context, typ *listerv1.GetIdentityRequest[*dummyv1.Repository]) (*listerv1.GetIdentityResponse, error) {
	logger.Debug("GetIdentity", "url", typ.Typ.BaseUrl)
	return nil, nil
}

var logger *slog.Logger

func main() {
	args := os.Args[1:]
	// log messages are shared over stderr by convention established by the plugin manager.
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug, // debug level here is respected when sending this message.
	}))

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	if err := componentlister.RegisterComponentLister(&dummyv1.Repository{}, &TestPlugin{}, capabilities); err != nil {
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

	logger.Info("starting up plugin", "extra", "info")

	if err := ocmPlugin.Start(context.Background()); err != nil {
		logger.Error("failed to start plugin", "error", err)
		os.Exit(1)
	}
}
