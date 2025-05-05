package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type Plugin struct{}

func (m *Plugin) Ping(_ context.Context) error {
	return nil
}

func (m *Plugin) GetComponentVersion(ctx context.Context, request repov1.GetComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Descriptor, error) {
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

func (m *Plugin) GetLocalResource(ctx context.Context, request repov1.GetLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) error {
	_, _ = fmt.Fprintf(os.Stdout, "Writing my local resource here to target: %+v\n", request.TargetLocation)
	return nil
}

func (m *Plugin) AddLocalResource(ctx context.Context, request repov1.PostLocalResourceRequest[*v1.OCIRepository], credentials map[string]string) (*descriptor.Resource, error) {
	_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource: %+v\n", request.ResourceLocation)
	return nil, nil
}

func (m *Plugin) AddComponentVersion(ctx context.Context, request repov1.PostComponentVersionRequest[*v1.OCIRepository], credentials map[string]string) error {
	_, _ = fmt.Fprintf(os.Stdout, "AddComponentVersiont: %+v\n", request.Descriptor.Component.Name)
	return nil
}

var _ repov1.ReadWriteOCMRepositoryPluginContract[*v1.OCIRepository] = &Plugin{}

func main() {
	args := os.Args[1:]

	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	if err := componentversionrepository.RegisterComponentVersionRepository(&v1.OCIRepository{}, &Plugin{}, capabilities); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// TODO(Skarlso): ConsumerIdentityTypesForConfig endpoint

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := json.Marshal(capabilities)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	// Parse command-line arguments
	configData := flag.String("config", "", "Plugin config.")
	flag.Parse()
	if configData == nil || *configData == "" {
		fmt.Fprintln(os.Stderr, "Missing required flag --config")
		os.Exit(1)
	}

	conf := types.Config{}
	if err := json.Unmarshal([]byte(*configData), &conf); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if conf.ID == "" {
		fmt.Fprintln(os.Stderr, "Plugin ID is required")
		os.Exit(1)
	}

	ocmPlugin := plugin.NewPlugin(conf, os.Stdout)
	if err := ocmPlugin.RegisterHandlers(capabilities.GetHandlers()...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := ocmPlugin.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
