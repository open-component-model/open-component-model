package e2e

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	genericspecv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var configPath string

func init() {
	flag.StringVar(&configPath, "e2e-config", "testdata/e2e-config.yaml", "Path to e2e test configuration YAML file")
}

// ParseConfig parses the specified yaml file using the generic configuration scheme.
func ParseConfig() *genericspecv1.Config {
	if !flag.Parsed() {
		flag.Parse()
	}

	genericCfg := &genericspecv1.Config{}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("Failed to read e2e config file: %v\n", err)
			os.Exit(1)
		}

		if err := DefaultScheme.Decode(bytes.NewReader(data), genericCfg); err != nil {
			fmt.Printf("Failed to decode e2e config file: %v\n", err)
			os.Exit(1)
		}
	}

	return genericCfg
}

func DecodeProvider(raw *runtime.Raw) (runtime.Typed, error) {
	if raw == nil {
		return nil, fmt.Errorf("provider configuration is missing")
	}
	spec, err := DefaultScheme.NewObject(raw.Type)
	if err != nil {
		return nil, fmt.Errorf("unknown provider type %s: %w", raw.Type, err)
	}
	if err := DefaultScheme.Convert(raw, spec); err != nil {
		return nil, fmt.Errorf("failed to convert provider spec: %w", err)
	}
	return spec, nil
}

// RegistryProviderConfig wraps the polymorphic registry provider configuration.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type RegistryProviderConfig struct {
	Type     runtime.Type `json:"type"`
	Provider *runtime.Raw `json:"provider"`
}

// ClusterProviderConfig wraps the polymorphic cluster provider configuration.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type ClusterProviderConfig struct {
	Type     runtime.Type `json:"type"`
	Provider *runtime.Raw `json:"provider"`
}

// CLIProviderConfig wraps the polymorphic CLI provider configuration.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CLIProviderConfig struct {
	Type     runtime.Type `json:"type"`
	Provider *runtime.Raw `json:"provider"`
}

// ZotProviderSpec configures a Zot registry provider.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type ZotProviderSpec struct {
	Type    runtime.Type `json:"type"`
	Version string       `json:"version,omitempty"`
}

// KindProviderSpec configures a Kind cluster provider.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type KindProviderSpec struct {
	Type    runtime.Type `json:"type"`
	Version string       `json:"version,omitempty"`
}

// ImageCLIProviderSpec configures an OCM CLI provider that runs in a container image.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type ImageCLIProviderSpec struct {
	Type runtime.Type `json:"type"`
	Path string       `json:"path,omitempty"` // Example: ghcr.io/open-component-model/cli@sha256:...
}

// BinaryCLIProviderSpec configures an OCM CLI provider that runs a local binary.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type BinaryCLIProviderSpec struct {
	Type runtime.Type `json:"type"`
	Path string       `json:"path,omitempty"` // Example: /usr/local/bin/ocm
}
