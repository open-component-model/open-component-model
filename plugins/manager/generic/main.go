package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"

	"ocm.software/open-component-model/plugins/manager"
	"ocm.software/open-component-model/plugins/plugin"
)

func main() {
	args := os.Args[1:]

	handlers, content, err := manager.NewOCMComponentVersionRepository(
		"CommonTransportFormat/v1",
		manager.TransferPlugin,
		manager.OCMComponentVersionRepositoryHandlersOpts{
			UploadComponentVersion:   nil, // provide your handlers
			DownloadComponentVersion: nil,
			UploadResource:           nil,
			DownloadResource:         nil,
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
