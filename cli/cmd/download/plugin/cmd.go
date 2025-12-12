package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types/spec"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagResourceVersion     = "resource-version"
	FlagOutput              = "output"
	FlagOutputFormat        = "output-format"
	FlagPluginType          = "plugin-type"
	FlagExtraIdentity       = "extra-identity"
	SkipValidation          = "skip-validation"
	pluginValidationTimeout = 30 * time.Second
)

// PluginType is the type of the resource containing the plugin in the component version.
// This type has been established by OCM v1 here:
// https://github.com/open-component-model/ocm/blob/bccf3310af0665eaab3f0ea9803e6b903d858d52/api/ocm/extensions/artifacttypes/const.go#L40
const PluginType = "ocmPlugin"

// pluginDirectoryDefault contains all plugins for ocm.
var pluginDirectoryDefault = filepath.Join(os.Getenv("HOME"), ".config", "ocm", "plugins")

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
		Example: ` # Download a plugin binary with resource name 'helminput' and version 'v0.0.0-main'
  ocm download plugin ghcr.io/open-component-model/plugins//ocm.software/plugins/helminput:0.0.0-main

  # Download a platform-specific plugin binary with extra identity parameters with specified output location.
  ocm download plugin ghcr.io/open-component-model/plugins//ocm.software/plugins/helminput:0.0.0-main --extra-identity os=linux,arch=amd64 --output ./plugins/ocm-plugin-linux-amd64`,
		RunE:              DownloadPlugin,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceVersion, "", "version of the plugin resource to download (optional, defaults to component version)")
	cmd.Flags().String(FlagOutput, "", "output folder to download the plugin binary to (default $HOME/.config/ocm/plugins)")
	enum.VarP(cmd.Flags(), FlagOutputFormat, "f", []string{"table", "yaml", "json"}, "output format of the plugin information, defaults to table")
	cmd.Flags().StringSlice(FlagExtraIdentity, []string{}, "extra identity parameters for resource matching (e.g., os=linux,arch=amd64)")
	cmd.Flags().Bool(SkipValidation, false, "skip validation of the downloaded plugin binary")
	cmd.Flags().String(FlagPluginType, PluginType, "type of the plugin resource in the component version containing the plugin binary")

	return cmd
}

func DownloadPlugin(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}
	ocmContext := ocmctx.FromContext(ctx)
	if ocmContext == nil {
		return fmt.Errorf("no OCM context found")
	}

	resourceVersion, err := cmd.Flags().GetString(FlagResourceVersion)
	if err != nil {
		return fmt.Errorf("getting resource-version flag failed: %w", err)
	}

	output, err := cmd.Flags().GetString(FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}
	if output == "" {
		output = pluginDirectoryDefault
	}

	skipValidation, err := cmd.Flags().GetBool(SkipValidation)
	if err != nil {
		return fmt.Errorf("getting skip-validation flag failed: %w", err)
	}

	outputFormat, err := enum.Get(cmd.Flags(), FlagOutputFormat)
	if err != nil {
		outputFormat = "table"
	}

	extraIdentitySlice, err := cmd.Flags().GetStringSlice(FlagExtraIdentity)
	if err != nil {
		return fmt.Errorf("getting extra-identity flag failed: %w", err)
	}
	extraIdentity, err := parseExtraIdentity(extraIdentitySlice)
	if err != nil {
		return err
	}

	reference := args[0]
	// we have a reference and parse it
	ref, err := compref.Parse(reference)
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", reference, err)
	}
	config := ocmContext.Configuration()
	slog.DebugContext(ctx, "parsed component reference", "reference", reference, "parsed", ref)

	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(cmd.Context(), pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, config, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(cmd.Context(), ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(cmd.Context(), ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("getting component version failed: %w", err)
	}

	resourceType, err := cmd.Flags().GetString(FlagPluginType)
	if err != nil {
		return fmt.Errorf("getting resource type flag failed: %w", err)
	}
	if resourceType == "" {
		resourceType = PluginType
	}

	// Build resource identity for matching
	resourceIdentity := ocmruntime.Identity{}

	if resourceVersion != "" {
		resourceIdentity["version"] = resourceVersion
	} else {
		resourceIdentity["version"] = desc.Component.Version
		logger.Info("using component version for resource version", slog.String("version", desc.Component.Version))
	}

	for key, value := range extraIdentity {
		resourceIdentity[key] = value
	}

	// Default OS and ARCH if not provided via --extra-identity
	if _, hasOS := extraIdentity["os"]; !hasOS {
		resourceIdentity["os"] = runtime.GOOS
		logger.Debug("defaulting os to runtime OS", slog.String("os", runtime.GOOS))
	}
	if _, hasArch := extraIdentity["architecture"]; !hasArch {
		resourceIdentity["architecture"] = runtime.GOARCH
		logger.Debug("defaulting arch to runtime ARCH", slog.String("architecture", runtime.GOARCH))
	}

	var toDownload []descriptor.Resource
	for _, resource := range desc.Component.Resources {
		// Type is not part of the identity so the below identity matcher will not
		// catch that, hence, we do this here.
		if resource.Type != resourceType {
			continue
		}

		// if the type matches we have our resource; we set the name for the identity match.
		resourceIdentity["name"] = resource.Name

		resourceIdent := resource.ToIdentity()
		if resourceIdentity.Match(resourceIdent, ocmruntime.IdentityMatchingChainFn(ocmruntime.IdentitySubset)) {
			toDownload = append(toDownload, resource)
		}
	}

	if len(toDownload) == 0 {
		return fmt.Errorf("no resource found matching identity %v", resourceIdentity)
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

	data, err := shared.DownloadResourceData(ctx, pluginManager, credentialGraph, ref.Component, ref.Version, repo, res, resourceIdentity)
	if err != nil {
		return fmt.Errorf("downloading plugin resource for identity %q failed: %w", resourceIdentity, err)
	}

	// ocm.software/plugins/[helminput]
	pluginFileName := path.Base(ref.Component)
	output = filepath.Join(output, pluginFileName)

	if err := shared.SaveBlobToFile(data, output); err != nil {
		return err
	}

	tryToMakePluginExecutableOrWarn(output, logger)

	if !skipValidation {
		if err := validatePlugin(output, logger); err != nil {
			if removeErr := os.Remove(output); removeErr != nil {
				logger.Warn("failed to remove invalid plugin binary", slog.String("path", output), slog.String("error", removeErr.Error()))
			}
			return fmt.Errorf("downloaded binary is not a valid plugin: %w", err)
		}
	}

	logger.Info("plugin binary downloaded successfully", slog.String("output", output))

	// Display plugin information in requested format
	reader, size, err := encodePluginInfo(res, desc.Component.String(), output, outputFormat)
	if err != nil {
		return fmt.Errorf("generating plugin information output failed: %w", err)
	}

	if _, err := io.CopyN(cmd.OutOrStdout(), reader, size); err != nil {
		return fmt.Errorf("writing plugin information failed: %w", err)
	}

	return nil
}

type Info struct {
	Plugin   string `json:"plugin"`
	Version  string `json:"version"`
	Source   string `json:"source"`
	Type     string `json:"type"`
	Identity string `json:"identity"`
	Location string `json:"location"`
}

func encodePluginInfo(res *descriptor.Resource, source, outputPath, format string) (io.Reader, int64, error) {
	identity := res.ToIdentity()
	info := Info{
		Plugin:   res.Name,
		Version:  res.Version,
		Source:   source,
		Type:     res.Type,
		Identity: identity.String(),
		Location: outputPath,
	}

	var data []byte
	var err error

	switch format {
	case "json":
		data, err = json.MarshalIndent(info, "", "  ")
		if err != nil {
			return nil, 0, fmt.Errorf("marshaling plugin info as JSON failed: %w", err)
		}
		data = append(data, '\n') // Add newline for consistency
	case "yaml":
		data, err = yaml.Marshal(info)
		if err != nil {
			return nil, 0, fmt.Errorf("marshaling plugin info as YAML failed: %w", err)
		}
	case "table":
		fallthrough
	default:
		var buf bytes.Buffer
		t := table.NewWriter()
		t.SetStyle(table.StyleLight)
		t.SetOutputMirror(&buf)
		t.AppendHeader(table.Row{"PLUGIN", "VERSION", "SOURCE", "TYPE", "IDENTITY", "LOCATION"})
		t.AppendRow(table.Row{
			info.Plugin,
			info.Version,
			info.Source,
			info.Type,
			info.Identity,
			info.Location,
		})
		t.Render()
		data = buf.Bytes()
	}

	return bytes.NewReader(data), int64(len(data)), nil
}

func parseExtraIdentity(extraIdentitySlice []string) (map[string]string, error) {
	extraIdentity := make(map[string]string)
	for _, param := range extraIdentitySlice {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid extra-identity parameter format %q, expected key=value", param)
		}
		extraIdentity[parts[0]] = parts[1]
	}
	return extraIdentity, nil
}

func tryToMakePluginExecutableOrWarn(outputPath string, logger *slog.Logger) {
	if info, err := os.Stat(outputPath); err == nil && info.Mode().IsRegular() {
		if err := os.Chmod(outputPath, 0o755); err != nil {
			logger.Warn("failed to make plugin binary executable", slog.String("path", outputPath), slog.String("error", err.Error()))
		} else {
			logger.Info("made plugin binary executable", slog.String("path", outputPath))
		}
	}
}

func validatePlugin(pluginPath string, logger *slog.Logger) error {
	logger.Info("validating plugin binary", slog.String("path", pluginPath))

	ctx, cancel := context.WithTimeout(context.Background(), pluginValidationTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pluginPath, "capabilities")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("plugin capabilities command failed: %w", err)
	}

	var capabilities spec.PluginSpec
	if err := json.Unmarshal(output, &capabilities); err != nil {
		return fmt.Errorf("plugin capabilities returned invalid JSON: %w", err)
	}

	if len(capabilities.CapabilitySpecs) == 0 {
		return fmt.Errorf("plugin capabilities missing required 'types' field or is empty")
	}

	logger.Info("plugin validation successful")

	return nil
}
