//go:build wasip1

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/extism/go-pdk"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var pluginConfig mtypes.Config
var filesystemConfig *filesystemv1alpha1.Config

func init() {
	// Read OCM plugin config from manifest if available
	configStr, ok := pdk.GetConfig("ocm_config")
	if ok && configStr != "" {
		if err := json.Unmarshal([]byte(configStr), &pluginConfig); err != nil {
			pdk.Log(pdk.LogError, "failed to unmarshal plugin config: "+err.Error())
			return
		}

		// Parse filesystem config from plugin config
		var err error
		filesystemConfig, err = parseFilesystemConfig(pluginConfig)
		if err != nil {
			pdk.Log(pdk.LogError, "failed to parse filesystem config: "+err.Error())
		}
	}
}

//go:wasmexport process_resource
func ProcessResource() int32 {
	input := pdk.Input()

	var request *v1.ProcessResourceInputRequest
	if err := json.Unmarshal(input, &request); err != nil {
		pdk.SetError(err)
		return 1
	}

	response, err := processHelmResource(context.Background(), request, map[string]string{}, filesystemConfig)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	output, err := json.Marshal(response)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	pdk.Output(output)
	return 0
}

//go:wasmexport process_source
func ProcessSource() int32 {
	pdk.SetError(errors.New("not implemented"))
	return 1
}

//go:wasmexport capabilities
func Capabilities() int32 {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&helmv1.Helm{},
		runtime.NewVersionedType(helmv1.Type, helmv1.Version),
		runtime.NewUnversionedType(helmv1.Type),
	)

	capabilities := endpoints.NewEndpoints(scheme)
	content, err := capabilities.MarshalJSON()
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	output, err := json.Marshal(content)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	pdk.Output(output)
	return 0
}

func main() {}

// parseFilesystemConfig extracts filesystem configuration from plugin config
func parseFilesystemConfig(conf mtypes.Config) (*filesystemv1alpha1.Config, error) {
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

// processHelmResource wraps the helm.InputMethod to process resources using Extism HTTP
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

	helmSpec := helmv1.Helm{}
	helmScheme := runtime.NewScheme()
	helmScheme.MustRegisterWithAlias(&helmv1.Helm{},
		runtime.NewVersionedType(helmv1.Type, helmv1.Version),
		runtime.NewUnversionedType(helmv1.Type),
	)
	if err := helmScheme.Convert(resource.Input, &helmSpec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	httpClient := &http.Client{
		Transport: &ExtismRoundTripper{},
	}
	helmBlob, chart, err := getV1HelmBlobWasm(ctx, helmSpec, tempDir, credentials, httpClient)
	if err != nil {
		return nil, fmt.Errorf("error getting helm blob: %w", err)
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

	if err := blob.Copy(tmp, helmBlob); err != nil {
		return nil, fmt.Errorf("error copying blob data: %w", err)
	}

	var mediaType string
	if mtAware, ok := helmBlob.(blob.MediaTypeAware); ok {
		if mt, known := mtAware.MediaType(); known && mt != "" {
			mediaType = mt
		}
	}

	resourceMeta := &descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{
				Name:    request.Resource.Name,
				Version: request.Resource.Version,
			},
		},
	}

	if chart != nil {
		if resourceMeta.Name == "" {
			resourceMeta.Name = chart.Name
		}
		if resourceMeta.Version == "" {
			resourceMeta.Version = chart.Version
		}
	}

	return &v1.ProcessResourceInputResponse{
		Resource: resourceMeta,
		Location: &mtypes.Location{
			LocationType: mtypes.LocationTypeLocalFile,
			Value:        tmp.Name(),
			MediaType:    mediaType,
		},
	}, nil
}

// getV1HelmBlobWasm is a WASM-specific version of helminput.GetV1HelmBlob that uses
// our custom HTTP transport via Extism
func getV1HelmBlobWasm(ctx context.Context, helmSpec helmv1.Helm, tmpDir string, credentials map[string]string, httpClient *http.Client) (blob.ReadOnlyBlob, *helminput.ReadOnlyChart, error) {
	// For now, we only support local helm charts in WASM
	// Remote fetching with custom HTTP transport would require more integration work
	// with the helm downloader
	if helmSpec.Path == "" {
		return nil, nil, fmt.Errorf("WASM helm plugin currently only supports local charts (path must be specified)")
	}

	opts := []helminput.Option{}
	if len(credentials) > 0 {
		opts = append(opts, helminput.WithCredentials(credentials))
	}

	return helminput.GetV1HelmBlob(ctx, helmSpec, tmpDir, opts...)
}
