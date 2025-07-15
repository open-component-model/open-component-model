package resource

import (
	"errors"
	"fmt"
	"log/slog"
	"mime"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/log"
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

	identity, err := cmd.Flags().GetString(FlagResourceIdentity)
	if err != nil {
		return fmt.Errorf("getting res identities flag failed: %w", err)
	}

	output, err := cmd.Flags().GetString(FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	transformer, err := cmd.Flags().GetString(FlagTransformer)
	if err != nil {
		return fmt.Errorf("getting transformer flag failed: %w", err)
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

	requestedIdentity, err := runtime.ParseIdentity(identity)
	if err != nil {
		return fmt.Errorf("parsing res identity %q failed: %w", identity, err)
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
		return fmt.Errorf("expected exactly one res candidate to download, got %d", len(toDownload))
	}
	res := toDownload[0]

	access := res.GetAccess()
	var data blob.ReadOnlyBlob
	if isLocal(access) {
		data, _, err = repo.GetLocalResource(cmd.Context(), requestedIdentity)
	} else {
		var plugin resource.Repository
		plugin, err = pluginManager.ResourcePluginRegistry.GetResourcePlugin(cmd.Context(), access)
		if err != nil {
			return fmt.Errorf("getting res plugin for access %q failed: %w", access.GetType(), err)
		}
		var creds map[string]string
		if identity, err := plugin.GetResourceCredentialConsumerIdentity(cmd.Context(), &res); err == nil {
			if creds, err = credentialGraph.Resolve(cmd.Context(), identity); err != nil {
				return fmt.Errorf("getting credentials for res %q failed: %w", res.Name, err)
			}
		}
		data, err = plugin.DownloadResource(cmd.Context(), &res, creds)
	}
	if err != nil {
		return fmt.Errorf("downloading res %q failed: %w", res.Name, err)
	}

	if output == "" {
		output = requestedIdentity.String()
		// if we have media type aware data, we try to append the file extension based on the media type
		if mediaTypeAware, ok := data.(blob.MediaTypeAware); ok {
			if mediaType, known := mediaTypeAware.MediaType(); known {
				if extensions, err := mime.ExtensionsByType(mediaType); err == nil && len(extensions) > 0 {
					output += extensions[0]
				}
			}
		}
		logger.Warn("no output location specified, using res identity as output file name", slog.String("output", output))
	}

	if transformer == "" {
		// If no transformer is specified, we try to write the res as is.
		return filesystem.CopyBlobToOSPath(data, output)
	}

	// TODO(jakobmoellerdev): now lookup a transformer based on the transformer config
	//  then use the transformer config to look for a transformer plugin
	//  then call the transformer plugin to transform the res
	//  write the transformed res to the output location

	return fmt.Errorf("download based on transformer %q is not implemented", transformer)
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
