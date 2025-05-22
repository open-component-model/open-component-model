package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TestPlugin struct{}

func (m *TestPlugin) Ping(_ context.Context) error {
	return nil
}

func (m *TestPlugin) GetComponentVersion(ctx context.Context, request repov1.GetComponentVersionRequest[*dummyv1.Repository], credentials map[string]string) (*descriptor.Descriptor, error) {
	return &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
			Provider: runtime.Identity{
				"name": "ocm.software",
			},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-resource",
							Version: "1.0.0",
						},
					},
					SourceRefs: nil,
					Type:       "ociImage",
					Relation:   "local",
					Access: &runtime.Raw{
						Type: runtime.Type{
							Name: "ociArtifact",
						},
						Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
					},
					Digest: &descriptor.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "OciArtifactDigest",
						Value:                  "abcdef1234567890",
					},
					Size: 1024,
				},
			},
		},
	}, nil
}

func (m *TestPlugin) ListComponentVersions(ctx context.Context, request repov1.ListComponentVersionsRequest[*dummyv1.Repository], credentials map[string]string) ([]string, error) {
	return []string{"v0.0.1", "v0.0.2"}, nil
}

func (m *TestPlugin) GetLocalResource(ctx context.Context, request repov1.GetLocalResourceRequest[*dummyv1.Repository], credentials map[string]string) error {
	_, _ = fmt.Fprintf(os.Stdout, "Writing my local resource here to target: %+v\n", request.TargetLocation)
	return nil
}

func (m *TestPlugin) AddLocalResource(ctx context.Context, request repov1.PostLocalResourceRequest[*dummyv1.Repository], credentials map[string]string) (*descriptor.Resource, error) {
	_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource: %+v\n", request.ResourceLocation)
	return nil, nil
}

func (m *TestPlugin) AddComponentVersion(ctx context.Context, request repov1.PostComponentVersionRequest[*dummyv1.Repository], credentials map[string]string) error {
	_, _ = fmt.Fprintf(os.Stdout, "AddComponentVersiont: %+v\n", request.Descriptor.Component.Name)
	return nil
}

func (m *TestPlugin) GetIdentity(ctx context.Context, typ repov1.GetIdentityRequest[*dummyv1.Repository]) (runtime.Identity, error) {
	_, _ = fmt.Fprintf(os.Stdout, "GetIdentity: %+v\n", typ.Typ.BaseUrl)
	return nil, nil
}

var _ repov1.ReadWriteOCMRepositoryPluginContract[*dummyv1.Repository] = &TestPlugin{}

// Config defines a configuration that this plugin requires.
type Config struct {
	MaximumNumberOfPotatoes string `json:"maximumNumberOfPotatoes"`
}

func main() {
	args := os.Args[1:]
	// log messages are shared over stderr by convention established by the plugin manager.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo, // debug level here is respected when sending this message.
	}))

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	if err := componentversionrepository.RegisterComponentVersionRepository(&dummyv1.Repository{}, &TestPlugin{}, capabilities); err != nil {
		logger.Error("failed to register test plugin", "error", err.Error())
		os.Exit(1)
	}

	logger.Info("registered test plugin")

	capabilities.AddConfigType(runtime.Type{
		Name:    "custom.config",
		Version: "v1",
	})

	// TODO(Skarlso): ConsumerIdentityTypesForConfig endpoint

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

	for _, raw := range conf.ConfigTypes {
		pluginConfig := &Config{}
		if err := json.Unmarshal(raw.Data, pluginConfig); err != nil {
			logger.Error("failed to unmarshal plugin config", "error", err)
			os.Exit(1)
		}

		logger.Info("configuration successfully marshaled", "maximumNumberOfPotatoes", pluginConfig.MaximumNumberOfPotatoes)
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
