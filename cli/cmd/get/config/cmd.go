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

type EffectiveConfig struct {
	Filesystem *filesystemv1alpha1.Config `json:"filesystem,omitempty" yaml:"filesystem,omitempty"`
	HTTP       *httpv1alpha1.Config       `json:"http,omitempty"       yaml:"http,omitempty"`
	OCM        *ocmv1.Config              `json:"ocm,omitempty"        yaml:"ocm,omitempty"` //nolint:staticcheck // displaying deprecated config for user visibility
	Resolvers  *resolversv1alpha1.Config  `json:"resolvers,omitempty"  yaml:"resolvers,omitempty"`
	Ownership  *ownershipv1alpha1.Config  `json:"ownership,omitempty"  yaml:"ownership,omitempty"`
	Extract    *extractv1alpha1.Config    `json:"extract,omitempty"    yaml:"extract,omitempty"`
	Plugins    *pluginsv2alpha1.Config    `json:"plugins,omitempty"    yaml:"plugins,omitempty"`
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"configuration", "cfg"},
		Short:   "Get Evaluated Config For Actual Command Call",
		Long: `Evaluate the command line arguments and all explicitly
  or implicitly used configuration files and provide
  a single configuration object.`,
		Example:           fmt.Sprintf(`ocm get config --%s json`, FlagOutput),
		RunE:              GetConfig,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String()}, "output format")

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

func getEffectiveConfig(cfg *genericv1.Config) (*EffectiveConfig, error) {
	effective := &EffectiveConfig{}

	if hasEntries(cfg, filesystemv1alpha1.ConfigType, filesystemv1alpha1.Version) {
		if fs, err := filesystemv1alpha1.LookupConfig(cfg); err != nil {
			return nil, fmt.Errorf("filesystem: %w", err)
		} else {
			effective.Filesystem = fs
		}
	}

	if hasEntries(cfg, httpv1alpha1.ConfigType, httpv1alpha1.Version) {
		if http, err := httpv1alpha1.LookupConfig(cfg); err != nil {
			return nil, fmt.Errorf("http: %w", err)
		} else {
			effective.HTTP = http
		}
	}

	if hasEntries(cfg, ocmv1.ConfigType, ocmv1.Version) {
		if ocm, err := ocmv1.Lookup(cfg); err != nil { //nolint:staticcheck // displaying deprecated config for user visibility
			return nil, fmt.Errorf("ocm: %w", err)
		} else {
			effective.OCM = ocm
		}
	}

	if hasEntries(cfg, resolversv1alpha1.ConfigType, resolversv1alpha1.Version) {
		if resolvers, err := resolversv1alpha1.Lookup(cfg); err != nil {
			return nil, fmt.Errorf("resolvers: %w", err)
		} else {
			effective.Resolvers = resolvers
		}
	}

	if hasEntries(cfg, ownershipv1alpha1.ConfigType, ownershipv1alpha1.Version) {
		if ownership, err := ownershipv1alpha1.Lookup(cfg); err != nil {
			return nil, fmt.Errorf("ownership: %w", err)
		} else {
			effective.Ownership = ownership
		}
	}

	if hasEntries(cfg, extractv1alpha1.ConfigType, extractv1alpha1.Version) {
		if extract, err := extractv1alpha1.LookupConfig(cfg); err != nil {
			return nil, fmt.Errorf("extract: %w", err)
		} else {
			effective.Extract = extract
		}
	}

	if hasEntries(cfg, pluginsv2alpha1.ConfigType, pluginsv2alpha1.Version) {
		if plugins, err := pluginsv2alpha1.LookupConfig(cfg); err != nil {
			return nil, fmt.Errorf("plugins: %w", err)
		} else {
			effective.Plugins = plugins
		}
	}

	// if hasEntries(cfg, credentialsv1.ConfigType, credentialsv1.Version) {
	//	if creds, err := credentialsRuntime.LookupCredentialConfig(cfg); err != nil {
	//		return nil, fmt.Errorf("credentials: %w", err)
	//	} else {
	//		effective.Credentials = creds
	//	}
	//}

	return effective, nil
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
