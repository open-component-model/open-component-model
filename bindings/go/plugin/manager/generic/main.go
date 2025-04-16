package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"os"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type MyShitThatImplementsTheRightCapabilityContract[T runtime.Typed] struct {
	manager.EmptyBasePlugin
}

// This is what Jakob ment that the request here contains everything you need.
func (m *MyShitThatImplementsTheRightCapabilityContract[T]) GetComponentVersion(ctx context.Context, request manager.GetComponentVersionRequest[*v1.OCIRepository], credentials manager.Attributes) (*descriptor.Descriptor, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MyShitThatImplementsTheRightCapabilityContract[T]) GetLocalResource(ctx context.Context, request manager.GetLocalResourceRequest[*v1.OCIRepository], credentials manager.Attributes) error {
	//TODO implement me
	panic("implement me")
}

// Fuck. This contract is on the calling side... How would this implement stuff?
var _ manager.ReadOCMRepositoryPluginContract = &MyShitThatImplementsTheRightCapabilityContract[*v1.OCIRepository]{}

func main() {
	args := os.Args[1:]

	// The plugin type will be inferred from the capability. A single binary could implement MULTIPLE plugin types.
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	capabilityBuilder := manager.NewCapabilityBuilder(scheme)

	//
	//if err := manager.RegisterReadComponentVersionRepositoryCapability(capabilityBuilder, &v1.OCIRepository{}, manager.ReadComponentVersionRepositoryHandlersOpts[*v1.OCIRepository]{
	//	GetComponentVersion: GetComponentVersion,
	//	DownloadResource:    DownloadResource,
	//}); err != nil {
	//	log.Fatal(err)
	//}

	// &v1.OCIRepository{} -> infers the type for the implementation.
	// The struct passed in here will implement the right interface.
	if err := manager.RegisterCapability(capabilityBuilder, &v1.OCIRepository{}, &MyShitThatImplementsTheRightCapabilityContract{}); err != nil {
		log.Fatal(err)
	}

	if len(args) > 0 && args[0] == "capabilities" {
		if err := capabilityBuilder.PrintCapabilities(); err != nil {
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
