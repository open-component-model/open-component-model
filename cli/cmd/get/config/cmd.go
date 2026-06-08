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

func getEffectiveConfig(cfg *genericv1.Config) (*genericv1.Config, error) {
	result := &genericv1.Config{
		Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.Version),
	}

	entries := []struct {
		scheme *runtime.Scheme
		lookup func() (runtime.Typed, error)
	}{
		{filesystemv1alpha1.Scheme, func() (runtime.Typed, error) { return filesystemv1alpha1.LookupConfig(cfg) }},
		{httpv1alpha1.Scheme, func() (runtime.Typed, error) { return httpv1alpha1.LookupConfig(cfg) }},
		{ocmv1.Scheme, func() (runtime.Typed, error) { return ocmv1.Lookup(cfg) }}, //nolint:staticcheck // displaying deprecated config for user visibility
		{resolversv1alpha1.Scheme, func() (runtime.Typed, error) { return resolversv1alpha1.Lookup(cfg) }},
		{ownershipv1alpha1.Scheme, func() (runtime.Typed, error) { return ownershipv1alpha1.Lookup(cfg) }},
		{extractv1alpha1.Scheme, func() (runtime.Typed, error) { return extractv1alpha1.LookupConfig(cfg) }},
		{pluginsScheme, func() (runtime.Typed, error) { return pluginsv2alpha1.LookupConfig(cfg) }},
		// TODO: credentials config has json:"-" on all fields, serialization expectation needs to be clarified
		// {credentialsScheme, func() (runtime.Typed, error) { return credentialsRuntime.LookupCredentialConfig(cfg) }},
	}

	for _, e := range entries {
		if err := appendEffective(result, cfg, e.scheme, e.lookup); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// TODO: remove once pluginsv2alpha1.Scheme is exported (currently unexported var scheme)
var pluginsScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&pluginsv2alpha1.Config{}, runtime.NewVersionedType(pluginsv2alpha1.ConfigType, pluginsv2alpha1.Version))
	return s
}()

func appendEffective(result *genericv1.Config, cfg *genericv1.Config, scheme *runtime.Scheme, lookup func() (runtime.Typed, error)) error {
	var types []runtime.Type
	for typ, aliases := range scheme.GetTypes() {
		types = append(types, typ)
		types = append(types, aliases...)
	}
	if !hasEntries(cfg, types) {
		return nil
	}
	typed, err := lookup()
	if err != nil {
		return fmt.Errorf("%s: %w", types[0], err)
	}
	if typed == nil {
		return nil
	}
	raw := &runtime.Raw{}
	if err := scheme.Convert(typed, raw); err != nil {
		return fmt.Errorf("failed to convert %s to raw: %w", types[0], err)
	}
	result.Configurations = append(result.Configurations, raw)
	return nil
}

func hasEntries(cfg *genericv1.Config, types []runtime.Type) bool {
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: types,
	})
	if err != nil {
		return false
	}
	return len(filtered.Configurations) > 0
}
