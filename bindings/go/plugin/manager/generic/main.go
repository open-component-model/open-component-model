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
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type OCIPlugin[T runtime.Typed] struct{}

func (m *OCIPlugin[T]) Ping(_ context.Context) error {
	return nil
}

func (m *OCIPlugin[T]) GetComponentVersion(ctx context.Context, request manager.GetComponentVersionRequest[*v1.OCIRepository], credentials manager.Attributes) (*descriptor.Descriptor, error) {
	_, _ = fmt.Fprintf(os.Stdout, "Returning a descriptor: %+v\n", request.Name)
	return nil, nil
}

func (m *OCIPlugin[T]) GetLocalResource(ctx context.Context, request manager.GetLocalResourceRequest[*v1.OCIRepository], credentials manager.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "Writing my local resource here to target: %+v\n", request.TargetLocation)
	return nil
}

func (m *OCIPlugin[T]) AddLocalResource(ctx context.Context, request manager.PostLocalResourceRequest[T], credentials manager.Attributes) (*descriptor.Resource, error) {
	_, _ = fmt.Fprintf(os.Stdout, "AddLocalResource: %+v\n", request.ResourceLocation)
	return nil, nil
}

func (m *OCIPlugin[T]) AddComponentVersion(ctx context.Context, request manager.PostComponentVersionRequest[T], credentials manager.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "AddComponentVersiont: %+v\n", request.Descriptor.Component.Name)
	return nil
}

var _ manager.ReadWriteOCMRepositoryPluginContract[*v1.OCIRepository] = &OCIPlugin[*v1.OCIRepository]{}

func main() {
	args := os.Args[1:]

	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	capabilityBuilder := manager.NewCapabilityBuilder(scheme)

	if err := manager.RegisterSupportedForEndpoints(capabilityBuilder, &v1.OCIRepository{}, &OCIPlugin[*v1.OCIRepository]{}); err != nil {
		log.Fatal(err)
	}

	if len(args) > 0 && args[0] == "capabilities" {
		if err := capabilityBuilder.FinalizeEndpoints(); err != nil {
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

	conf := manager.Config{}
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
	if err := ocmPlugin.RegisterHandlers(capabilityBuilder.GetHandlers()...); err != nil {
		log.Fatal(err)
	}

	if err := ocmPlugin.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
