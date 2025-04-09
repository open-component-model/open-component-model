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
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/plugins/manager"
	"ocm.software/open-component-model/plugins/plugin"
)

// MyType assume this type lives in binding/go or some other place in OCM.
type MyType struct {
	runtime.Type `json:"type"`
	BaseUrl      string `json:"baseUrl"`
	SubPath      string `json:"subPath"`
}

func (o *MyType) GetType() runtime.Type {
	return o.Type
}

func (o *MyType) SetType(t runtime.Type) {
	o.Type = t
}

func (o *MyType) DeepCopyTyped() runtime.Typed {
	return &MyType{}
}

// GetComponentVersion implements component version fetching.
func GetComponentVersion[T runtime.Typed](ctx context.Context, name string, version string, registry T, credentials runtime.Identity, writer io.Writer) (err error) {
	fmt.Printf("GetComponentVersion[%s %s]\n", name, version)

	return nil
}

func DownloadResource[T runtime.Typed](ctx context.Context, request *manager.GetResourceRequest, credentials runtime.Identity, writer io.Writer) error {
	fmt.Printf("DownloadResource[%s %s]\n", request.Name, request.Version)

	return nil
}

func UploadResource[T runtime.Typed](ctx context.Context, request *manager.PostResourceRequest, credentials runtime.Identity, writer io.Writer) error {
	fmt.Printf("UploadResource[%s %s]\n", request.Resource.Name, request.Resource.Version)

	return nil
}

func UploadComponentVersion[T runtime.Typed](ctx context.Context, descriptor *descriptor.Descriptor, registry T, credentials runtime.Identity) error {
	fmt.Printf("UploadComponentVersion[%s %s]\n", descriptor.Component.Name, descriptor.Component.Version)

	return nil
}

func main() {
	args := os.Args[1:]

	typ := &MyType{
		Type: runtime.Type{
			Version: "v1",
			Name:    "OCIRegistry",
		},
		BaseUrl: "url",
		SubPath: "/path",
	}
	handlers, content, err := manager.NewReadWriteComponentVersionRepository(
		typ,
		manager.TransferPlugin,
		manager.ReadWriteComponentVersionRepositoryHandlersOpts[*MyType]{
			UploadComponentVersion: UploadComponentVersion[*MyType], // provide your handlers
			GetComponentVersion:    GetComponentVersion[*MyType],
			UploadResource:         UploadResource[*MyType],
			DownloadResource:       DownloadResource[*MyType],
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	if len(args) > 0 && args[0] == "capabilities" {
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
	if err := ocmPlugin.RegisterHandlers(handlers.GetHandlers()...); err != nil {
		log.Fatal(err)
	}

	if err := ocmPlugin.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
