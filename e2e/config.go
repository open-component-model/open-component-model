package e2e

import (
	"flag"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

var configPath string

func init() {
	flag.StringVar(&configPath, "e2e-config", "", "Path to e2e test configuration YAML file")
}

// Config holds the configuration for the e2e tests.
type Config struct {
	OCM      OCMConfig      `json:"ocm"`
	Registry RegistryConfig `json:"registry"`
	Cluster  ClusterConfig  `json:"cluster"`
}

// OCMConfig configurations for the OCM CLI.
type OCMConfig struct {
	// Source indicates how to get the OCM CLI: "binary" or "image".
	// For "binary", Path is the local path.
	// For "image", Path is the container image reference.
	Source string `json:"source"`
	Path   string `json:"path"`
}

// RegistryConfig configurations for the OCI Registry.
type RegistryConfig struct {
	// Provider is the type of registry (e.g., "zot").
	Provider string `json:"provider"`
	// Version is the version/tag of the registry image.
	Version string `json:"version"`
}

// ClusterConfig configurations for the Kubernetes Cluster.
type ClusterConfig struct {
	// Provider is the type of cluster (e.g., "kind").
	Provider string `json:"provider"`
	// Version is the Kubernetes version.
	Version string `json:"version"`
}

// ParseConfig parses the specified yaml file to populate Config.
func ParseConfig() *Config {
	cfg := &Config{
		OCM: OCMConfig{
			Source: "image",
			Path:   "ghcr.io/open-component-model/cli@sha256:e2571d4df8b1816075a169c0b4e6851cae59b75f4f786f2a00bb832ee0db868e",
		},
		Registry: RegistryConfig{
			Provider: "zot",
			Version:  "latest",
		},
		Cluster: ClusterConfig{
			Provider: "kind",
			Version:  "kindest/node:v1.29.2",
		},
	}

	// Parse flags if they haven't been parsed yet.
	// In Go testing, flags are often parsed by the test runner.
	if !flag.Parsed() {
		flag.Parse()
	}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("Failed to read e2e config file: %v\n", err)
			os.Exit(1)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			fmt.Printf("Failed to parse e2e config file: %v\n", err)
			os.Exit(1)
		}
	}

	return cfg
}
