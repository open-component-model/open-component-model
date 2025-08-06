package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagResourceName    = "resource-name"
	FlagResourceVersion = "resource-version"
	FlagOutput          = "output"
	FlagExtraIdentity   = "extra-identity"
	SkipValidation      = "skip-validation"
	timeout             = 30 * time.Second
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plugin",
		Aliases: []string{"plugins"},
		Short:   "Download plugin binaries from a component version.",
		Args:    cobra.ExactArgs(1),
		Long: `Download a plugin binary from a component version located in a component version.

This command fetches a specific plugin resource from the given OCM component version and stores it at the specified output location.
The plugin binary can be identified by resource name and version, with optional extra identity parameters for platform-specific binaries.

Resources can be accessed either locally or via a plugin that supports remote fetching, with optional credential resolution.`,
		Example: ` # Download a plugin binary with resource name 'ocm-plugin' and version 'v1.0.0'
  ocm download plugin ghcr.io/org/component:v1 --resource-name ocm-plugin --resource-version v1.0.0 --output ./plugins/ocm-plugin

  # Download a platform-specific plugin binary with extra identity parameters
  ocm download plugin ghcr.io/org/component:v1 --resource-name ocm-plugin --resource-version v1.0.0 --extra-identity os=linux,arch=amd64 --output ./plugins/ocm-plugin-linux-amd64

  # Download plugin using only resource name (uses component version if resource version not specified)
  ocm download plugin ghcr.io/org/component:v1 --resource-name ocm-plugin --output ./plugins/ocm-plugin`,
		RunE:              DownloadPlugin,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceName, "", "name of the plugin resource to download (required)")
	cmd.Flags().String(FlagResourceVersion, "", "version of the plugin resource to download (optional, defaults to component version)")
	cmd.Flags().String(FlagOutput, ".", "output location to download the plugin binary to (required)")
	cmd.Flags().StringSlice(FlagExtraIdentity, []string{}, "extra identity parameters for resource matching (e.g., os=linux,arch=amd64)")
	cmd.Flags().Bool(SkipValidation, false, "skip validation of the downloaded plugin binary")

	_ = cmd.MarkFlagRequired(FlagResourceName)
	_ = cmd.MarkFlagRequired(FlagOutput)

	return cmd
}

func DownloadPlugin(cmd *cobra.Command, args []string) error {
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("could not retrieve logger: %w", err)
	}

	resourceName, err := cmd.Flags().GetString(FlagResourceName)
	if err != nil {
		return fmt.Errorf("getting resource-name flag failed: %w", err)
	}

	resourceVersion, err := cmd.Flags().GetString(FlagResourceVersion)
	if err != nil {
		return fmt.Errorf("getting resource-version flag failed: %w", err)
	}

	output, err := cmd.Flags().GetString(FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	extraIdentitySlice, err := cmd.Flags().GetStringSlice(FlagExtraIdentity)
	if err != nil {
		return fmt.Errorf("getting extra-identity flag failed: %w", err)
	}

	skipValidation, err := cmd.Flags().GetBool(SkipValidation)
	if err != nil {
		return fmt.Errorf("getting skip-validation flag failed: %w", err)
	}

	// Parse extra identity parameters
	extraIdentity := make(map[string]string)
	for _, param := range extraIdentitySlice {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid extra-identity parameter format %q, expected key=value", param)
		}
		extraIdentity[parts[0]] = parts[1]
	}

	reference := args[0]
	repo, err := ocm.NewFromRef(cmd.Context(), pluginManager, credentialGraph, reference)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(cmd.Context())
	if err != nil {
		return fmt.Errorf("getting component version failed: %w", err)
	}

	// Build resource identity for matching
	resourceIdentity := runtime.Identity{
		"name": resourceName,
	}

	// Add a resource version if specified, otherwise use component version
	if resourceVersion != "" {
		resourceIdentity["version"] = resourceVersion
	} else {
		resourceIdentity["version"] = desc.Component.Version
		logger.Info("using component version for resource version", slog.String("version", desc.Component.Version))
	}

	// Add extra identity parameters
	for key, value := range extraIdentity {
		resourceIdentity[key] = value
	}

	// Find a matching resource
	var toDownload []descriptor.Resource
	for _, resource := range desc.Component.Resources {
		resourceIdent := resource.ToIdentity()
		if resourceIdentity.Match(resourceIdent, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			toDownload = append(toDownload, resource)
		}
	}

	if len(toDownload) == 0 {
		return fmt.Errorf("no plugin resource found matching identity %v", resourceIdentity)
	}
	if len(toDownload) > 1 {
		logger.Warn("multiple resources match identity, using first match", slog.Int("count", len(toDownload)))
	}
	res := &toDownload[0]

	logger.Info("downloading plugin resource",
		slog.String("name", res.Name),
		slog.String("version", res.Version),
		slog.String("type", res.Type),
		slog.Any("identity", res.ToIdentity()))

	access := res.GetAccess()
	var data blob.ReadOnlyBlob
	if isLocal(access) {
		data, _, err = repo.GetLocalResource(cmd.Context(), resourceIdentity)
	} else {
		var plugin resource.Repository
		plugin, err = pluginManager.ResourcePluginRegistry.GetResourcePlugin(cmd.Context(), access)
		if err != nil {
			return fmt.Errorf("getting resource plugin for access %q failed: %w", access.GetType(), err)
		}
		var creds map[string]string
		if identity, err := plugin.GetResourceCredentialConsumerIdentity(cmd.Context(), res); err == nil {
			if creds, err = credentialGraph.Resolve(cmd.Context(), identity); err != nil {
				return fmt.Errorf("getting credentials for resource %q failed: %w", res.Name, err)
			}
		}
		data, err = plugin.DownloadResource(cmd.Context(), res, creds)
	}
	if err != nil {
		return fmt.Errorf("downloading plugin resource for identity %q failed: %w", resourceIdentity, err)
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(output)
	if outputDir != "." && outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("creating output directory %q failed: %w", outputDir, err)
		}
	}

	if err := filesystem.CopyBlobToOSPath(data, output); err != nil {
		return fmt.Errorf("writing plugin binary to %q failed: %w", output, err)
	}

	// Make the binary executable if it's a regular file
	if info, err := os.Stat(output); err == nil && info.Mode().IsRegular() {
		if err := os.Chmod(output, 0o755); err != nil {
			logger.Warn("failed to make plugin binary executable", slog.String("path", output), slog.String("error", err.Error()))
		} else {
			logger.Info("made plugin binary executable", slog.String("path", output))
		}
	}

	if !skipValidation {
		if err := validatePlugin(output, logger); err != nil {
			if removeErr := os.Remove(output); removeErr != nil {
				logger.Warn("failed to remove invalid plugin binary", slog.String("path", output), slog.String("error", removeErr.Error()))
			}
			return fmt.Errorf("downloaded binary is not a valid plugin: %w", err)
		}
	}

	logger.Info("plugin binary downloaded successfully", slog.String("output", output))
	return nil
}

func isLocal(access runtime.Typed) bool {
	if access == nil {
		return false
	}
	var local v2.LocalBlob
	if err := v2.Scheme.Convert(access, &local); err != nil {
		return false
	}
	return true
}

func validatePlugin(pluginPath string, logger *slog.Logger) error {
	logger.Info("validating plugin binary", slog.String("path", pluginPath))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pluginPath, "capabilities")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("plugin capabilities command failed: %w", err)
	}

	var capabilities types.Types
	if err := json.Unmarshal(output, &capabilities); err != nil {
		return fmt.Errorf("plugin capabilities returned invalid JSON: %w", err)
	}

	if len(capabilities.Types) == 0 {
		return fmt.Errorf("plugin capabilities missing required 'types' field or is empty")
	}

	logger.Info("plugin validation successful",
		slog.Int("plugin_types", len(capabilities.Types)),
		slog.Int("config_types", len(capabilities.ConfigTypes)))

	return nil
}
