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
	flag.StringVar(&configPath, "e2e-config", "", "Path to e2e test configuration YAML file")
}

// Config holds the configuration for the e2e tests.
type Config struct {
	Registry runtime.Typed
	Cluster  runtime.Typed
	CLI      runtime.Typed
}

// ParseConfig parses the specified yaml file using the generic configuration scheme.
func ParseConfig() *Config {
	if !flag.Parsed() {
		flag.Parse()
	}

	cfg := &Config{}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("Failed to read e2e config file: %v\n", err)
			os.Exit(1)
		}

		// Decode the outer generic config list
		genericCfg := &genericspecv1.Config{}
		if err := DefaultScheme.Decode(bytes.NewReader(data), genericCfg); err != nil {
			fmt.Printf("Failed to decode e2e config file: %v\n", err)
			os.Exit(1)
		}

		// Process each configuration
		for _, rawCfg := range genericCfg.Configurations {
			// Convert raw wrapper to a typed config wrapper (e.g., RegistryProviderConfig)
			obj, err := DefaultScheme.NewObject(rawCfg.Type)
			if err != nil {
				continue // not an e2e config wrapper
			}
			if err := DefaultScheme.Convert(rawCfg, obj); err != nil {
				fmt.Printf("Failed to convert raw config to wrapper: %v\n", err)
				os.Exit(1)
			}

			// Extract the provider spec based on the wrapper type
			switch wrapper := obj.(type) {
			case *RegistryProviderConfig:
				spec, err := decodeProvider(wrapper.Provider)
				if err != nil {
					fmt.Printf("Failed to decode registry provider spec: %v\n", err)
					os.Exit(1)
				}
				cfg.Registry = spec
			case *ClusterProviderConfig:
				spec, err := decodeProvider(wrapper.Provider)
				if err != nil {
					fmt.Printf("Failed to decode cluster provider spec: %v\n", err)
					os.Exit(1)
				}
				cfg.Cluster = spec
			case *CLIProviderConfig:
				spec, err := decodeProvider(wrapper.Provider)
				if err != nil {
					fmt.Printf("Failed to decode CLI provider spec: %v\n", err)
					os.Exit(1)
				}
				cfg.CLI = spec
			}
		}
	} else {
		// Provide reasonable defaults if no file provided
		cfg.Registry = &ZotProviderSpec{Version: "latest"}
		cfg.Cluster = &KindProviderSpec{Version: "kindest/node:v1.29.2"}
		cfg.CLI = &ImageCLIProviderSpec{Path: "ghcr.io/open-component-model/cli@sha256:e2571d4df8b1816075a169c0b4e6851cae59b75f4f786f2a00bb832ee0db868e"}
	}

	return cfg
}

func decodeProvider(raw *runtime.Raw) (runtime.Typed, error) {
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
