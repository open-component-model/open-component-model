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
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	customCredentialTypeName    = "DummyToken"
	customCredentialTypeVersion = "v1"
)

var customCredentialType = runtime.NewVersionedType(customCredentialTypeName, customCredentialTypeVersion)

type TestCredentialRepositoryPlugin struct{}

var _ v1.CredentialRepositoryPluginContract[*dummyv1.Repository] = (*TestCredentialRepositoryPlugin)(nil)

func (m *TestCredentialRepositoryPlugin) Ping(_ context.Context) error { return nil }

func (m *TestCredentialRepositoryPlugin) ConsumerIdentityForConfig(
	_ context.Context,
	cfg v1.ConsumerIdentityForConfigRequest[*dummyv1.Repository],
) (runtime.Identity, error) {
	return runtime.Identity{"type": customCredentialTypeName, "url": cfg.Config.BaseUrl}, nil
}

func (m *TestCredentialRepositoryPlugin) Resolve(
	_ context.Context,
	_ v1.ResolveRequest[*dummyv1.Repository],
	_ runtime.Typed,
) (runtime.Typed, error) {
	return &runtime.Raw{
		Type: customCredentialType,
		Data: []byte(`{"token":"resolved"}`),
	}, nil
}

func main() {
	args := os.Args[1:]
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	handler := &TestCredentialRepositoryPlugin{}
	proto := &dummyv1.Repository{}

	typ, err := capabilities.Scheme.TypeForPrototype(proto)
	if err != nil {
		logger.Error("failed to get type for prototype", "error", err)
		os.Exit(1)
	}
	schema, err := plugins.GenerateJSONSchemaForType(proto)
	if err != nil {
		logger.Error("failed to generate json schema", "error", err)
		os.Exit(1)
	}

	capabilities.Handlers = append(capabilities.Handlers,
		endpoints.Handler{
			Handler:  credentialrepository.ConsumerIdentityForConfigHandlerFunc(handler.ConsumerIdentityForConfig, capabilities.Scheme, proto),
			Location: credentialrepository.ConsumerIdentityForConfig,
		},
		endpoints.Handler{
			Handler:  credentialrepository.ResolveHandlerFunc(handler.Resolve, capabilities.Scheme, proto),
			Location: credentialrepository.Resolve,
		},
	)
	capabilities.PluginSpec.CapabilitySpecs = append(capabilities.PluginSpec.CapabilitySpecs, &v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.CredentialRepositoryPluginType)),
		SupportedCredentialRepositorySpecTypes: []types.Type{
			{Type: typ, JSONSchema: schema},
		},
		CustomCredentialTypes: []types.Type{
			{Type: customCredentialType},
		},
	})

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := capabilities.MarshalJSON()
		if err != nil {
			logger.Error("failed to marshal capabilities", "error", err)
			os.Exit(1)
		}

		if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
			logger.Error("failed to print capabilities", "error", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

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
	if err := ocmPlugin.Start(context.Background()); err != nil {
		logger.Error("failed to start plugin", "error", err)
		os.Exit(1)
	}
}
