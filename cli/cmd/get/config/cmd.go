package config

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	sigsyaml "sigs.k8s.io/yaml"

	extractv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/extract/v1alpha1/spec"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
	ocmv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	ownershipv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/ownership/v1alpha1/spec"
	resolversv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	pluginsv2alpha1 "ocm.software/open-component-model/cli/internal/plugin/spec/config/v2alpha1"
	"ocm.software/open-component-model/cli/internal/render"
)

const (
	FlagOutput = "output"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"configuration", "cfg"},
		Short:   "Display the effective merged OCM configuration",
		Long: `Evaluate the command line arguments and all explicitly or implicitly used
configuration files and display the merged effective configuration as a single object.`,
		Example: `  # Display effective config in YAML (default)
  ocm get config

  # Display effective config in JSON
  ocm get config --output json

  # Display effective config from a specific config file
  ocm get config --config ./my-ocm-config.yaml`,
		RunE:              GetConfig,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String()}, "output format")

	return cmd
}

func GetConfig(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}
	ocmContext := ocmctx.FromContext(cmd.Context())
	if ocmContext == nil {
		return fmt.Errorf("no OCM context found")
	}
	config := ocmContext.Configuration()
	if config == nil {
		return fmt.Errorf("no configuration found in context")
	}
	effectiveConfig, err := getEffectiveConfig(config)
	if err != nil {
		return fmt.Errorf("failed to determine effective configuration: %w", err)
	}
	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	switch output {
	case render.OutputFormatJSON.String():
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(effectiveConfig)
	case render.OutputFormatNDJSON.String():
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		return enc.Encode(effectiveConfig)
	case render.OutputFormatYAML.String():
		data, err := sigsyaml.Marshal(effectiveConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	default:
		return fmt.Errorf("unsupported output format: %s", output)
	}
}

type effectiveConfig struct {
	Type           runtime.Type `json:"type"`
	Configurations []any        `json:"configurations"`
}

func getEffectiveConfig(cfg *genericv1.Config) (*effectiveConfig, error) {
	result := &effectiveConfig{
		Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.Version),
	}

	var typed runtime.Typed
	var err error

	if hasEntries(cfg, filesystemv1alpha1.ConfigType, filesystemv1alpha1.Version) {
		typed, err = filesystemv1alpha1.LookupConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("config lookup failed for filesystem: %w", err)
		}
		result.Configurations = append(result.Configurations, typed)
	}

	if hasEntries(cfg, httpv1alpha1.ConfigType, httpv1alpha1.Version) {
		typed, err = httpv1alpha1.LookupConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("config lookup failed for http: %w", err)
		}
		result.Configurations = append(result.Configurations, typed)
	}

	if hasEntries(cfg, ocmv1.ConfigType, ocmv1.Version) {
		typed, err = ocmv1.Lookup(cfg) //nolint:staticcheck // displaying deprecated config for user visibility
		if err != nil {
			return nil, fmt.Errorf("config lookup failed for ocm: %w", err)
		}
		result.Configurations = append(result.Configurations, typed)
	}

	if hasEntries(cfg, resolversv1alpha1.ConfigType, resolversv1alpha1.Version) {
		typed, err = resolversv1alpha1.Lookup(cfg)
		if err != nil {
			return nil, fmt.Errorf("config lookup failed for resolvers: %w", err)
		}
		result.Configurations = append(result.Configurations, typed)
	}

	if hasEntries(cfg, ownershipv1alpha1.ConfigType, ownershipv1alpha1.Version) {
		typed, err = ownershipv1alpha1.Lookup(cfg)
		if err != nil {
			return nil, fmt.Errorf("config lookup failed for ownership: %w", err)
		}
		result.Configurations = append(result.Configurations, typed)
	}

	if hasEntries(cfg, pluginsv2alpha1.ConfigType, pluginsv2alpha1.Version) {
		typed, err = pluginsv2alpha1.LookupConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("config lookup failed for plugins: %w", err)
		}
		result.Configurations = append(result.Configurations, typed)
	}

	if hasEntries(cfg, extractv1alpha1.ConfigType, extractv1alpha1.Version) {
		// bindings/go/configuration/extract/v1alpha1/spec/config.go merge is no-op, so the result is always empty
		// Serialize the raw entry instead (caveat: output is not merged)
		filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
			ConfigTypes: []runtime.Type{
				runtime.NewVersionedType(extractv1alpha1.ConfigType, extractv1alpha1.Version),
				runtime.NewUnversionedType(extractv1alpha1.ConfigType),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to filter config for %s: %w", extractv1alpha1.ConfigType, err)
		}
		for _, entry := range filtered.Configurations {
			var config extractv1alpha1.Config
			if err := extractv1alpha1.Scheme.Convert(entry, &config); err != nil {
				return nil, fmt.Errorf("failed to decode credential config: %w", err)
			}
			result.Configurations = append(result.Configurations, &config)
		}
	}

	if hasEntries(cfg, credentialsv1.ConfigType, credentialsv1.Version) {
		// Runtime types of credentials serialize as `json:"-"` (bindings/go/credentials/spec/config/runtime/config.go)
		// Serialize the raw entry instead (caveat: output is not merged)
		filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
			ConfigTypes: []runtime.Type{
				runtime.NewVersionedType(credentialsv1.ConfigType, credentialsv1.Version),
				runtime.NewUnversionedType(credentialsv1.ConfigType),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to filter config for %s: %w", credentialsv1.ConfigType, err)
		}
		for _, entry := range filtered.Configurations {
			result.Configurations = append(result.Configurations, entry)
		}
	}

	return result, nil
}

func hasEntries(cfg *genericv1.Config, configType string, version string) bool {
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(configType, version),
			runtime.NewUnversionedType(configType),
		},
	})
	if err != nil {
		return false
	}
	return len(filtered.Configurations) > 0
}
