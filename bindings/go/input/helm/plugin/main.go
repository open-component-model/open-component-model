package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/input/helm"
	helmv1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type HelmInputPlugin struct{}

var logger *slog.Logger

func (h *HelmInputPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	logger.Info("GetIdentity called for Helm input", "type", typ.Typ)
	return nil, nil
}

func (h *HelmInputPlugin) ProcessResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceInputResponse, error) {
	logger.Info("ProcessResource called for Helm input")
	return processHelmResource(ctx, request, credentials)
}

func (h *HelmInputPlugin) ProcessSource(ctx context.Context, request *v1.ProcessSourceInputRequest, credentials map[string]string) (*v1.ProcessSourceInputResponse, error) {
	logger.Info("ProcessSource called for Helm input")
	return processHelmSource(ctx, request, credentials)
}

func (h *HelmInputPlugin) Ping(_ context.Context) error {
	return nil
}

var _ v1.ResourceInputPluginContract = &HelmInputPlugin{}

func main() {
	args := os.Args[1:]
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	scheme := runtime.NewScheme()
	// Register Helm input spec types
	scheme.MustRegisterWithAlias(&helmv1.Helm{},
		runtime.NewVersionedType(helmv1.Type, helmv1.Version),
		runtime.NewUnversionedType(helmv1.Type),
	)

	capabilities := endpoints.NewEndpoints(scheme)

	if err := input.RegisterInputProcessor(&helmv1.Helm{}, &HelmInputPlugin{}, capabilities); err != nil {
		logger.Error("failed to register helm input plugin", "error", err.Error())
		os.Exit(1)
	}

	logger.Info("registered helm input plugin")

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

	logger.Info("starting up helm input plugin", "plugin", conf.ID)

	if err := ocmPlugin.Start(context.Background()); err != nil {
		logger.Error("failed to start plugin", "error", err)
		os.Exit(1)
	}
}

// processHelmResource wraps the helm.InputMethod to process resources
func processHelmResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceInputResponse, error) {
	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: request.Resource.Input,
		},
	}

	helmMethod := &helm.InputMethod{}
	result, err := helmMethod.ProcessResource(ctx, resource, credentials)
	if err != nil {
		return nil, fmt.Errorf("helm input method failed: %w", err)
	}

	tmp, err := os.CreateTemp("", "helm-source-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	defer func() {
		if cerr := tmp.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	closer, err := result.ProcessedBlobData.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error reading blob data: %w", err)
	}

	if _, err := io.Copy(tmp, closer); err != nil {
		return nil, fmt.Errorf("error writing temp file: %w", err)
	}

	return &v1.ProcessResourceInputResponse{
		Resource: &constructorv1.Resource{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    request.Resource.Name,
					Version: request.Resource.Version,
				},
			},
			Type: "helmChart",
		},
		Location: &types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
		},
	}, nil
}

// processHelmSource wraps the helm.InputMethod to process sources
func processHelmSource(ctx context.Context, request *v1.ProcessSourceInputRequest, credentials map[string]string) (_ *v1.ProcessSourceInputResponse, err error) {
	return nil, fmt.Errorf("not implemented")
}
