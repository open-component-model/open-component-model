package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	clicommandv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/clicommand/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/clicommand"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type testGreetPlugin struct{}

var _ clicommandv1.CLICommandPluginContract = &testGreetPlugin{}

func (p *testGreetPlugin) Ping(_ context.Context) error { return nil }

func (p *testGreetPlugin) GetCLICommandCredentialConsumerIdentity(
	_ context.Context,
	_ *clicommandv1.GetCredentialConsumerIdentityRequest,
) (*clicommandv1.GetCredentialConsumerIdentityResponse, error) {
	return &clicommandv1.GetCredentialConsumerIdentityResponse{
		Identity: runtime.Identity{"service": "test"},
	}, nil
}

func (p *testGreetPlugin) Execute(
	_ context.Context,
	req *clicommandv1.ExecuteRequest,
	_ map[string]string,
) (*clicommandv1.ExecuteResponse, error) {
	name := "world"
	if v, ok := req.Flags["name"]; ok && v != "" {
		name = v
	}
	return &clicommandv1.ExecuteResponse{
		Output: fmt.Sprintf("hello, %s!\n", name),
	}, nil
}

func main() {
	args := os.Args[1:]
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	scheme := runtime.NewScheme()
	capabilities := endpoints.NewEndpoints(scheme)

	if err := clicommand.RegisterCLICommand(
		clicommandv1.CommandSpec{
			Verb:       "greet",
			ObjectType: "hello",
			Short:      "Say hello",
			Long:       "Greet someone by name.",
			Flags: []clicommandv1.FlagSpec{
				{Name: "name", Type: "string", Usage: "Name to greet", DefaultValue: "world"},
			},
		},
		&testGreetPlugin{},
		capabilities,
	); err != nil {
		logger.Error("failed to register test CLI command plugin", "error", err.Error())
		os.Exit(1)
	}

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := capabilities.MarshalJSON()
		if err != nil {
			logger.Error("failed to marshal capabilities", "error", err)
			os.Exit(1)
		}
		if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
			logger.Error("failed to print capabilities", "error", err)
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
	if conf.ID == "" {
		logger.Error("plugin config has no ID")
		os.Exit(1)
	}

	separateContext := context.Background()
	ocmPlugin := plugin.NewPlugin(separateContext, logger, conf, os.Stdout)
	if err := ocmPlugin.RegisterHandlers(capabilities.GetHandlers()...); err != nil {
		logger.Error("failed to register handlers", "error", err)
		os.Exit(1)
	}
	if err := ocmPlugin.Start(context.Background()); err != nil {
		logger.Error("failed to start plugin", "error", err)
		os.Exit(1)
	}
}
