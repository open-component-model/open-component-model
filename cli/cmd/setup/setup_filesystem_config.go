package setup

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

func overrideTempFolder(cmd *cobra.Command, fsCfg *filesystemv1alpha1.Config, value string) {
	if fsCfg.TempFolder != "" {
		slog.WarnContext(cmd.Context(), "temp folder was defined in ocm config with value, will be overwritten by value", slog.String("original", fsCfg.TempFolder), slog.String("new", value))
	}

	fsCfg.TempFolder = value
}

func overrideWorkingDirectory(cmd *cobra.Command, fsCfg *filesystemv1alpha1.Config, value string) {
	if fsCfg.WorkingDirectory != "" {
		slog.WarnContext(cmd.Context(), "working-directory was defined in ocm config with value, will be overwritten by value", slog.String("original", fsCfg.WorkingDirectory), slog.String("new", value))
	}

	fsCfg.WorkingDirectory = value
}

func ensureFilesystemConfig(cmd *cobra.Command, cfg *genericv1.Config, fsCfg *filesystemv1alpha1.Config) {
	// If we have a CLI flag but no filesystem config in the config,
	// we need to add it to the configuration
	if cfg != nil && !hasFilesystemConfig(cfg) {
		if err := addFilesystemConfigToCentralConfig(cmd, fsCfg); err != nil {
			slog.WarnContext(cmd.Context(), "could not add filesystem config to central configuration", slog.String("error", err.Error()))
		}
	}
}

type FilesystemConfigOptions struct {
	WorkingDirectory string
	TempFolder       string
}

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&filesystemv1alpha1.Config{},
		runtime.NewVersionedType(filesystemv1alpha1.ConfigType, filesystemv1alpha1.Version))
	scheme.MustRegisterWithAlias(&filesystemv1alpha1.Config{},
		runtime.NewUnversionedType(filesystemv1alpha1.ConfigType))
}

// FilesystemConfig sets up file system configuration entity.
func FilesystemConfig(cmd *cobra.Command, opts FilesystemConfigOptions) {
	ocmCtx := ocmctx.FromContext(cmd.Context())
	cfg := ocmCtx.Configuration()
	var fsCfg *filesystemv1alpha1.Config
	if cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize filesystem config")
		fsCfg = &filesystemv1alpha1.Config{}
	} else {
		if _fsCfg, err := filesystemv1alpha1.LookupConfig(cfg); err != nil {
			slog.DebugContext(cmd.Context(), "could not get filesystem configuration", slog.String("error", err.Error()))
			fsCfg = &filesystemv1alpha1.Config{}
		} else {
			fsCfg = _fsCfg
		}
	}
	_, _ = scheme.DefaultType(fsCfg)

	// Manually set CLI flags takes precedence over the config file or over any passed options
	// (i.e. from child commands)
	if flag := cmd.Flags().Lookup(ocmcmd.TempFolderFlag); flag != nil && flag.Changed {
		overrideTempFolder(cmd, fsCfg, flag.Value.String())
	} else if opts.TempFolder != "" {
		overrideTempFolder(cmd, fsCfg, opts.TempFolder)
	}

	if flag := cmd.Flags().Lookup(ocmcmd.WorkingDirectoryFlag); flag != nil && flag.Changed {
		overrideWorkingDirectory(cmd, fsCfg, flag.Value.String())
	} else if opts.WorkingDirectory != "" {
		overrideWorkingDirectory(cmd, fsCfg, opts.WorkingDirectory)
	}

	ensureFilesystemConfig(cmd, cfg, fsCfg)

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
	if err := scheme.Convert(fsCfg, raw); err != nil {
		return fmt.Errorf("failed to convert filesystem config to raw: %w", err)
	}
	cfg.Configurations = append(cfg.Configurations, raw)

	ctx := ocmctx.WithConfiguration(cmd.Context(), cfg)
	cmd.SetContext(ctx)

	return nil
}
