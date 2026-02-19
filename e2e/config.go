package e2e

import (
	"flag"
)

// Config holds the configuration for the e2e tests.
type Config struct {
	OCM      OCMConfig
	Registry RegistryConfig
	Cluster  ClusterConfig
}

// OCMConfig configurations for the OCM CLI.
type OCMConfig struct {
	// Source indicates how to get the OCM CLI: "binary" or "image".
	// For "binary", Path is the local path.
	// For "image", Path is the container image reference.
	Source string
	Path   string
}

// RegistryConfig configurations for the OCI Registry.
type RegistryConfig struct {
	// Provider is the type of registry (e.g., "zot").
	Provider string
	// Version is the version/tag of the registry image.
	Version string
}

// ClusterConfig configurations for the Kubernetes Cluster.
type ClusterConfig struct {
	// Provider is the type of cluster (e.g., "kind").
	Provider string
	// Version is the Kubernetes version.
	Version string
}

// ParseConfig parses command-line flags and environment variables to populate Config.
func ParseConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.OCM.Source, "ocm-source", "image", "Source of OCM CLI: 'binary' or 'image'")
	flag.StringVar(&cfg.OCM.Path, "ocm-path", "ghcr.io/open-component-model/cli@sha256:e2571d4df8b1816075a169c0b4e6851cae59b75f4f786f2a00bb832ee0db868e", "Path to OCM binary or image reference")

	flag.StringVar(&cfg.Registry.Provider, "registry-provider", "zot", "Registry provider type")
	flag.StringVar(&cfg.Registry.Version, "registry-version", "latest", "Registry version/tag")

	flag.StringVar(&cfg.Cluster.Provider, "cluster-provider", "kind", "Cluster provider type")
	flag.StringVar(&cfg.Cluster.Version, "cluster-version", "kindest/node:v1.29.2", "Kubernetes cluster version")

	// Parse flags if they haven't been parsed yet.
	// In Go testing, flags are often parsed by the test runner.
	if !flag.Parsed() {
		flag.Parse()
	}

	return cfg
}
