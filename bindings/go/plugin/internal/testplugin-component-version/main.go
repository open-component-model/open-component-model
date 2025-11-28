package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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

var _ repov1.ReadWriteOCMRepositoryPluginContract[*dummyv1.Repository] = (*TestPlugin)(nil)

func (m *TestPlugin) Ping(_ context.Context) error {
	return nil
}

func (m *TestPlugin) CheckHealth(ctx context.Context, request repov1.PostCheckHealthRequest[*dummyv1.Repository], credentials map[string]string) error {
	// Would construct request.BaseURL and try to ping the repository here.
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
			Provider: descriptor.Provider{
				Name: "ocm.software",
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
				},
			},
		},
	}, nil
}

func (m *TestPlugin) ListComponentVersions(ctx context.Context, request repov1.ListComponentVersionsRequest[*dummyv1.Repository], credentials map[string]string) ([]string, error) {
	return []string{"v0.0.1", "v0.0.2"}, nil
}

func (m *TestPlugin) GetLocalResource(ctx context.Context, request repov1.GetLocalResourceRequest[*dummyv1.Repository], credentials map[string]string) (repov1.GetLocalResourceResponse, error) {
	// the plugin decides where things will live.
	f, err := os.CreateTemp("", "test-resource-file")
	if err != nil {
		return repov1.GetLocalResourceResponse{}, fmt.Errorf("error creating temp file: %w", err)
	}

	if err := os.WriteFile(f.Name(), []byte("test-resource"), 0o600); err != nil {
		return repov1.GetLocalResourceResponse{}, fmt.Errorf("error write to temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		return repov1.GetLocalResourceResponse{}, fmt.Errorf("error closing temp file: %w", err)
	}

	logger.Debug("writing local file here", "location", f.Name())
	return repov1.GetLocalResourceResponse{
		Location: types.Location{
			Value:        f.Name(),
			LocationType: types.LocationTypeLocalFile,
		},
		Resource: &v2.Resource{
			ElementMeta: v2.ElementMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "test-resource",
					Version: "v0.0.1",
				},
			},
			Type:     "resource-type",
			Relation: "local",
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    "test-access",
					Version: "v1",
				},
				Data: []byte(`{ "access": "v1" }`),
			},
			Digest: &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "jsonNormalisation/v1",
				Value:                  "test-value",
			},
		},
	}, nil
}

func (m *TestPlugin) GetLocalSource(ctx context.Context, request repov1.GetLocalSourceRequest[*dummyv1.Repository], credentials map[string]string) (repov1.GetLocalSourceResponse, error) {
	// the plugin decides where things will live.
	f, err := os.CreateTemp("", "test-source-file")
	if err != nil {
		return repov1.GetLocalSourceResponse{}, fmt.Errorf("error creating temp file: %w", err)
	}

	if err := os.WriteFile(f.Name(), []byte("test-source"), 0o600); err != nil {
		return repov1.GetLocalSourceResponse{}, fmt.Errorf("error write to temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		return repov1.GetLocalSourceResponse{}, fmt.Errorf("error closing temp file: %w", err)
	}

	logger.Debug("writing local file here", "location", f.Name())
	return repov1.GetLocalSourceResponse{
		Location: types.Location{
			Value:        f.Name(),
			LocationType: types.LocationTypeLocalFile,
		},
		Source: &v2.Source{
			ElementMeta: v2.ElementMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "test-source",
					Version: "v0.0.1",
				},
			},
			Type: "source-type",
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    "test-access",
					Version: "v1",
				},
				Data: []byte(`{ "access": "v1" }`),
			},
		},
	}, nil
}

func (m *TestPlugin) AddLocalResource(ctx context.Context, request repov1.PostLocalResourceRequest[*dummyv1.Repository], credentials map[string]string) (*descriptor.Resource, error) {
	logger.Debug("AddLocalResource", "location", request.ResourceLocation)
	return nil, nil
}

func (m *TestPlugin) AddLocalSource(ctx context.Context, request repov1.PostLocalSourceRequest[*dummyv1.Repository], credentials map[string]string) (*descriptor.Source, error) {
	logger.Debug("AddLocalSource", "location", request.SourceLocation)
	return nil, nil
}

func (m *TestPlugin) AddComponentVersion(ctx context.Context, request repov1.PostComponentVersionRequest[*dummyv1.Repository], credentials map[string]string) error {
	logger.Debug("AddComponentVersion", "name", request.Descriptor.Component.Name)
	return nil
}

func (m *TestPlugin) GetIdentity(ctx context.Context, typ *repov1.GetIdentityRequest[*dummyv1.Repository]) (*repov1.GetIdentityResponse, error) {
	logger.Debug("GetIdentity", "url", typ.Typ.BaseUrl)
	return nil, nil
}

var _ repov1.ReadWriteOCMRepositoryPluginContract[*dummyv1.Repository] = &TestPlugin{}

// Config defines a configuration that this plugin requires.
type Config struct {
	MaximumNumberOfPotatoes string `json:"maximumNumberOfPotatoes"`
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
