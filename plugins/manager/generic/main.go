package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/invopop/jsonschema"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/plugins/manager"
	"ocm.software/open-component-model/plugins/plugin"
)

func main() {
	v1 := &ctf.CommonTransportFormat{}
	generator := jsonschema.Reflect(v1)
	ctfSchema, err := generator.MarshalJSON()
	if err != nil {
		log.Fatal(err)
	}

	v2 := &oci.OCIArtifact{}
	generator = jsonschema.Reflect(v2)
	ociSchema, err := generator.MarshalJSON()
	if err != nil {
		log.Fatal(err)
	}

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "capabilities" {
		caps := &manager.Capabilities{
			Type: map[runtime.Type][]manager.Capability{
				"CommonTransportFormat/v1": {
					{
						Capability: manager.GenericRepositoryCapability,
						Schema:     ctfSchema,
					},
				},
				"OCIArtifact/v1": {
					{
						Capability: manager.ReadWriteComponentVersionRepositoryCapability,
						Schema:     ociSchema,
					},
					{
						Capability: manager.GenericRepositoryCapability,
						Schema:     ociSchema,
					},
				},
			},
		}

		content, err := json.Marshal(caps)
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
	if err := ocmPlugin.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
