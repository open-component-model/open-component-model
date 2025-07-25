package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

// setupFilesystemConfig sets up file system configuration entity.
func setupFilesystemConfig(cmd *cobra.Command) {
	var (
		value string
		err   error
	)
	if flag := cmd.Flags().Lookup(tempFolderFlag); flag != nil && flag.Changed {
		value, err = cmd.Flags().GetString(tempFolderFlag)
		if err != nil {
			slog.DebugContext(cmd.Context(), "could not read temp folder flag value", slog.String("error", err.Error()))
		}
	}

	ocmCtx := ocmctx.FromContext(cmd.Context())
	cfg := ocmCtx.Configuration()
	var fsCfg *filesystemv1alpha1.Config
	if cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize filesystem config")
		fsCfg = &filesystemv1alpha1.Config{}
	} else {
		fsCfg, err = filesystemv1alpha1.LookupConfig(cfg)
		if err != nil {
			slog.DebugContext(cmd.Context(), "could not get filesystem configuration", slog.String("error", err.Error()))
			fsCfg = &filesystemv1alpha1.Config{}
		}
	}

	// CLI flag takes precedence over the config file
	if value != "" {
		if fsCfg.TempFolder != "" {
			slog.WarnContext(cmd.Context(), "temp folder was defined in ocm config with value, will be overwritten by value", slog.String("original", fsCfg.TempFolder), slog.String("new", value))
		}

		fsCfg.TempFolder = value

		// If we have a CLI flag but no filesystem config in the config,
		// we need to add it to the configuration
		if cfg != nil && !hasFilesystemConfig(cfg) {
			if err := addFilesystemConfigToCentralConfig(cmd, fsCfg); err != nil {
				slog.WarnContext(cmd.Context(), "could not add filesystem config to central configuration", slog.String("error", err.Error()))
			}
		}
	}

	ctx := ocmctx.WithFilesystemConfig(cmd.Context(), fsCfg)
	cmd.SetContext(ctx)
}

// hasFilesystemConfig checks if the central configuration already contains filesystem configuration
// It uses the Config Filter function to handle versioned configurations properly
func hasFilesystemConfig(cfg *genericv1.Config) bool {
	if cfg == nil {
		return false
	}

	// Use the Config Filter function to find filesystem configurations
	// This handles both versioned and unversioned configurations
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(filesystemv1alpha1.ConfigType, filesystemv1alpha1.Version),
			runtime.NewUnversionedType(filesystemv1alpha1.ConfigType),
		},
	})
	if err != nil {
		return false
	}

	return len(filtered.Configurations) > 0
}

// addFilesystemConfigToCentralConfig adds the filesystem configuration to the central configuration
func addFilesystemConfigToCentralConfig(cmd *cobra.Command, fsCfg *filesystemv1alpha1.Config) error {
	ocmCtx := ocmctx.FromContext(cmd.Context())
	cfg := ocmCtx.Configuration()
	if cfg == nil {
		return fmt.Errorf("no central configuration available")
	}

	raw := &runtime.Raw{}
	if err := genericv1.Scheme.Convert(fsCfg, raw); err != nil {
		return fmt.Errorf("failed to convert filesystem config to raw: %w", err)
	}
	cfg.Configurations = append(cfg.Configurations, raw)

	ctx := ocmctx.WithConfiguration(cmd.Context(), cfg)
	cmd.SetContext(ctx)

	return nil
}
