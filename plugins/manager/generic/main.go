package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/plugins/manager"
	"ocm.software/open-component-model/plugins/plugin"
)

// GetComponentVersion implements component version fetching.
func GetComponentVersion(ctx context.Context, name string, version string, registry runtime.Typed, credentials manager.Attributes, writer io.Writer) (err error) {
	_, _ = fmt.Fprintf(os.Stdout, "GetComponentVersion[%s %s]\n", name, version)

	return nil
}

func DownloadResource(ctx context.Context, request *manager.GetResourceRequest, credentials manager.Attributes, writer io.Writer) error {
	_, _ = fmt.Fprintf(os.Stdout, "DownloadResource[%s %s]\n", request.Name, request.Version)

	return nil
}

func UploadResource(ctx context.Context, request *manager.PostResourceRequest, credentials manager.Attributes, writer io.Writer) error {
	_, _ = fmt.Fprintf(os.Stdout, "UploadResource[%s %s]\n", request.Resource.Name, request.Resource.Version)

	return nil
}

func UploadComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor, registry runtime.Typed, credentials manager.Attributes) error {
	_, _ = fmt.Fprintf(os.Stdout, "UploadComponentVersion[%s %s]\n", descriptor.Component.Name, descriptor.Component.Version)

	return nil
}

func main() {
	args := os.Args[1:]

	// The plugin type will be inferred from the capability. A single binary could implement MULTIPLE plugin types.
	capabilityBuilder := manager.NewCapabilityBuilder()
	if err := capabilityBuilder.RegisterReadWriteComponentVersionRepositoryCapability(&v1.OCIImage{}, manager.ReadWriteComponentVersionRepositoryHandlersOpts{
		UploadComponentVersion: UploadComponentVersion, // provide your handlers
		GetComponentVersion:    GetComponentVersion,
		UploadResource:         UploadResource,
		DownloadResource:       DownloadResource,
	}); err != nil {
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
