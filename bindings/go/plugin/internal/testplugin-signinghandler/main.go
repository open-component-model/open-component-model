package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	plugin "ocm.software/open-component-model/bindings/go/plugin/client/sdk"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TestSigningPlugin struct{}

func (m *TestSigningPlugin) GetSignerIdentity(ctx context.Context, req *v1.GetSignerIdentityRequest[*dummyv1.Repository]) (*v1.IdentityResponse, error) {
	return &v1.IdentityResponse{Identity: map[string]string{"id": "signer"}}, nil
}

func (m *TestSigningPlugin) Sign(ctx context.Context, request *v1.SignRequest[*dummyv1.Repository], credentials map[string]string) (*v1.SignResponse, error) {
	return &v1.SignResponse{Signature: &v2.SignatureInfo{Algorithm: "rsa", Value: "sig", MediaType: "text/plain"}}, nil
}

func (m *TestSigningPlugin) GetVerifierIdentity(ctx context.Context, req *v1.GetVerifierIdentityRequest[*dummyv1.Repository]) (*v1.IdentityResponse, error) {
	return &v1.IdentityResponse{Identity: map[string]string{"id": "verifier"}}, nil
}

func (m *TestSigningPlugin) Verify(ctx context.Context, request *v1.VerifyRequest[*dummyv1.Repository], credentials map[string]string) (*v1.VerifyResponse, error) {
	return &v1.VerifyResponse{}, nil
}

func (m *TestSigningPlugin) Ping(_ context.Context) error { return nil }

var _ v1.SignatureHandlerContract[*dummyv1.Repository] = &TestSigningPlugin{}

func main() {
	args := os.Args[1:]
	// log messages are shared over stderr by convention established by the plugin manager.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)

	if err := signinghandler.RegisterPlugin(&dummyv1.Repository{}, &TestSigningPlugin{}, capabilities); err != nil {
		logger.Error("failed to register test signing plugin", "error", err.Error())
		os.Exit(1)
	}

	if len(args) > 0 && args[0] == "capabilities" {
		content, err := capabilities.MarshalJSON()
		if err != nil {
			logger.Error("failed to marshal capabilities", "error", err)
			os.Exit(1)
		}

		if _, err := fmt.Fprintln(os.Stdout, string(content)); err != nil {
			logger.Error("failed print capabilities", "error", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Parse command-line arguments
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
