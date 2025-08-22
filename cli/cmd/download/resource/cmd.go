package resource

import (
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"path/filepath"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagResourceIdentity = "identity"
	FlagOutput           = "output"
	FlagTransformer      = "transformer"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "resource",
		Aliases: []string{"resources"},
		Short:   "Download resources described in a component version in an OCM Repository",
		Args:    cobra.MaximumNArgs(1),
		Long: `Download a resource from a component version located in an Open Component Model (OCM) repository.

This command fetches a specific resource from the given OCM component version reference and stores it at the specified output location. 
It supports optional transformation of the resource using a registered transformer plugin.

If no transformer is specified, the resource is written directly in its original format. If the media type is known,
the appropriate file extension will be added to the output file name if no output location is given.

Resources can be accessed either locally or via a plugin that supports remote fetching, with optional credential resolution.`,
		Example: ` # Download a resource with identity 'name=example' and write to default output
  ocm resource ghcr.io/org/component:v1 --identity name=example

  # Download a resource and specify an output file
  ocm resource ghcr.io/org/component:v1 --identity name=example --output ./my-resource.tar.gz

  # Download a resource and apply a transformer
  ocm resource ghcr.io/org/component:v1 --identity name=example --transformer my-transformer`,
		RunE:              DownloadResource,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "resource identity to download")
	cmd.Flags().String(FlagOutput, "", "output location to download to. If no transformer is specified, and no "+
		"format was discovered that can be written to a directory, the resource will be written to a file.")
	cmd.Flags().String(FlagTransformer, "", "transformer to use for the output. If not specified, the resource will be written as is. ")

	return cmd
}

func init() {
	if err := errors.Join(
		mime.AddExtensionType(".tar.gz", layout.MediaTypeOCIImageLayoutTarGzipV1),
		mime.AddExtensionType(".tar", layout.MediaTypeOCIImageLayoutTarV1),
	); err != nil {
		panic(err)
	}
}

func DownloadResource(cmd *cobra.Command, args []string) error {
	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}

	identityStr, err := cmd.Flags().GetString(FlagResourceIdentity)
	if err != nil {
		return fmt.Errorf("getting resource identities flag failed: %w", err)
	}

	output, err := cmd.Flags().GetString(FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	transformer, err := cmd.Flags().GetString(FlagTransformer)
	if err != nil {
		return fmt.Errorf("getting transformer flag failed: %w", err)
	}

	requestedIdentity, err := runtime.ParseIdentity(identityStr)
	if err != nil {
		return fmt.Errorf("parsing resource identity %q failed: %w", identityStr, err)
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

	var toDownload []descriptor.Resource
	for _, resource := range desc.Component.Resources {
		resourceIdentity := resource.ToIdentity()
		if requestedIdentity.Match(resourceIdentity, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			toDownload = append(toDownload, resource)
			break
		}
	}

	if len(toDownload) != 1 {
		return fmt.Errorf("expected exactly one resource candidate to download, got %d", len(toDownload))
	}
	res := &toDownload[0]

	data, err := shared.DownloadResourceData(cmd.Context(), pluginManager, credentialGraph, repo, res, requestedIdentity)
	if err != nil {
		return fmt.Errorf("downloading resource for identity %q failed: %w", requestedIdentity, err)
	}

	finalOutput, err := processResourceOutput(output, res, data, requestedIdentity.String(), logger)
	if err != nil {
		return err
	}

	if transformer == "" {
		if err := shared.SaveBlobToFile(data, finalOutput); err != nil {
			return err
		}
		logger.Info("resource downloaded successfully", slog.String("output", finalOutput))
		return nil
	}

	// TODO(jakobmoellerdev): now lookup a transformer based on the transformer config
	//  then use the transformer config to look for a transformer plugin
	//  then call the transformer plugin to transform the res
	//  write the transformed res to the output location
	return fmt.Errorf("download based on transformer %q is not implemented", transformer)
}

func processResourceOutput(output string, resource *descriptor.Resource, data blob.ReadOnlyBlob, identity string, logger *slog.Logger) (string, error) {
	// Check for downloadName label
	for _, label := range resource.Labels {
		if label.Name == "downloadName" {
			var downloadName string
			if err := label.GetValue(&downloadName); err != nil {
				return "", fmt.Errorf("interpreting downloadName label value failed: %w", err)
			}
			if downloadName = filepath.Clean(downloadName); filepath.IsAbs(downloadName) {
				return "", fmt.Errorf("downloadName label value %q must not be an absolute path for security reasons", downloadName)
			}
			logger.Info("using downloadName label for file download location", slog.String("output", downloadName))
			return downloadName, nil
		}
	}

	if output == "" {
		output = identity
		// if we have media type aware data, we try to append the file extension based on the media type
		if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
			if mediaType, known := mediaTypeAware.MediaType(); known {
				if extensions, err := mime.ExtensionsByType(mediaType); err == nil && len(extensions) > 0 {
					output += extensions[0]
				}
			}
		}
		logger.Warn("no output location specified, using resource identity as output file name", slog.String("output", output))
	}

	return output, nil
}
