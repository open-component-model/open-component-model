package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type OCIPlugin struct{}

func (m *OCIPlugin) Ping(_ context.Context) error {
	return nil
}

func (m *OCIPlugin) GetComponentVersion(ctx context.Context, request types.GetComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Descriptor, error) {
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

func (m *OCIPlugin) GetLocalResource(ctx context.Context, request types.GetLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "Writing my local resource here to target: %+v\n", request.TargetLocation)
	return nil
}

func (m *OCIPlugin) AddLocalResource(ctx context.Context, request types.PostLocalResourceRequest[*v1.OCIRepository], credentials contracts.Attributes) (*descriptor.Resource, error) {
	_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource: %+v\n", request.ResourceLocation)
	return nil, nil
}

func (m *OCIPlugin) AddComponentVersion(ctx context.Context, request types.PostComponentVersionRequest[*v1.OCIRepository], credentials contracts.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "AddComponentVersiont: %+v\n", request.Descriptor.Component.Name)
	return nil
}

var _ contracts.ReadWriteOCMRepositoryPluginContract[*v1.OCIRepository] = &OCIPlugin{}

func main() {
	args := os.Args[1:]

	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	if err := componentversionrepository.RegisterComponentVersionRepository(&v1.OCIRepository{}, &OCIPlugin{}, capabilities); err != nil {
		log.Fatal(err)
	}

	// TODO: ConsumerIdentityTypesForConfig endpoint

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := json.Marshal(capabilities)
		if err != nil {
			log.Fatal(err)
		}

		if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
			log.Fatal(err)
		}

		os.Exit(0)
	}

	// Parse command-line arguments
	configData := flag.String("config", "", "Plugin config.")
	flag.Parse()
	if configData == nil || *configData == "" {
		log.Fatal("Missing required flag --config")
	}

	conf := types.Config{}
	if err := json.Unmarshal([]byte(*configData), &conf); err != nil {
		log.Fatal(err)
	}

	if conf.ID == "" {
		log.Fatal("Plugin ID is required.")
	}
	if conf.Location == "" {
		log.Fatal("Plugin location is required.")
	}
	r := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

	ocmPlugin := plugin.NewPlugin(r, conf)
	if err := ocmPlugin.RegisterHandlers(capabilities.GetHandlers()...); err != nil {
		log.Fatal(err)
	}

	if err := ocmPlugin.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
