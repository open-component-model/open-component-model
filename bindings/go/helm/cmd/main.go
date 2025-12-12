package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var helpContent = `
Helm Input Plugin for OCM

This plugin processes Helm charts as OCM component resources.

Usage:
 helm-plugin capabilities           Returns plugin capabilities (JSON)
 helm-plugin --config <json>        Starts the plugin server
 helm-plugin help                   Shows this help message

Examples:
 # Query plugin capabilities
 $ helm-plugin capabilities
 {"types":{"inputRepository":[{"type":"Helm/v1","jsonSchema":"..."}]}}

 # Start plugin (typically called by OCM plugin manager)
 $ helm-plugin --config '{"id":"helm","type":"unix","pluginType":"inputRepository",...}'
 http+unix:///tmp/helm-plugin.socket

Configuration:
 Accepts filesystem/v1alpha1 config for temp folder location.`

// HelmInputPlugin is a plugin that implements the helm.InputMethod interface as an external binary.
type HelmInputPlugin struct {
	contracts.EmptyBasePlugin
	filesystemConfig *filesystemv1alpha1.Config
}

var logger *slog.Logger

func (h *HelmInputPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	logger.Info("GetIdentity called for Helm input", "type", typ.Typ)
	return nil, nil
}

func (h *HelmInputPlugin) ProcessResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceInputResponse, error) {
	logger.Info("ProcessResource called for Helm input")
	return processHelmResource(ctx, request, credentials, h.filesystemConfig)
}

func (h *HelmInputPlugin) ProcessSource(ctx context.Context, request *v1.ProcessSourceInputRequest, credentials map[string]string) (*v1.ProcessSourceInputResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

var _ v1.ResourceInputPluginContract = &HelmInputPlugin{}

func main() {
	args := os.Args[1:]
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	capabilities := endpoints.NewEndpoints(helminput.Scheme)
	helmPlugin := &HelmInputPlugin{}
	if err := input.RegisterInputProcessor(&helmv1.Helm{}, helmPlugin, capabilities); err != nil {
		logger.Error("failed to register helm input plugin", "error", err.Error())
		os.Exit(1)
	}

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

	if len(args) > 0 && args[0] == "help" {
		if _, err := fmt.Fprintln(os.Stdout, helpContent); err != nil {
			logger.Error("failed print helm input", "error", err)
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
	logger.Debug("config data", "config", conf)

	if conf.ID == "" {
		logger.Error("plugin config has no ID")
		os.Exit(1)
	}

	// Parse filesystem config from plugin config
	filesystemConfig, err := parseFilesystemConfig(conf)
	if err != nil {
		logger.Error("failed to parse filesystem config", "error", err.Error())
		os.Exit(1)
	}

	// update to use the configuration
	helmPlugin.filesystemConfig = filesystemConfig

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

// parseFilesystemConfig extracts filesystem configuration from plugin config
func parseFilesystemConfig(conf types.Config) (*filesystemv1alpha1.Config, error) {
	if len(conf.ConfigTypes) == 0 {
		return &filesystemv1alpha1.Config{}, nil
	}

	// Convert plugin config types to generic config
	genericConfig := &genericv1.Config{
		Configurations: conf.ConfigTypes,
	}

	// Use LookupConfig to get filesystem config with defaults
	filesystemConfig, err := filesystemv1alpha1.LookupConfig(genericConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup filesystem config: %w", err)
	}

	return filesystemConfig, nil
}

// processHelmResource wraps the helm.InputMethod to process resources
func processHelmResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string, filesystemConfig *filesystemv1alpha1.Config) (_ *v1.ProcessResourceInputResponse, err error) {
	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: request.Resource.Input,
		},
	}

	tempDir := ""
	if filesystemConfig != nil {
		tempDir = filesystemConfig.TempFolder
	}

	helmMethod := &helminput.InputMethod{
		TempFolder: tempDir,
	}
	result, err := helmMethod.ProcessResource(ctx, resource, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to process resource: %w", err)
	}
	tmp, err := os.CreateTemp(tempDir, "helm-resource-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	defer func() {
		if cerr := tmp.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	if err := blob.Copy(tmp, result.ProcessedBlobData); err != nil {
		return nil, fmt.Errorf("error copying blob data: %w", err)
	}

	var mediaType string
	if mtAware, ok := result.ProcessedBlobData.(blob.MediaTypeAware); ok {
		if mt, known := mtAware.MediaType(); known && mt != "" {
			mediaType = mt
		}
	}

	return &v1.ProcessResourceInputResponse{
		Location: &types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
			MediaType:    mediaType,
		},
	}, nil
}
