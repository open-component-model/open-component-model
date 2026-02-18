package resource

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nlepage/go-tarfs"
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
	"ocm.software/open-component-model/cli/internal/transformers"
)

const (
	FlagResourceIdentity = "identity"
	FlagOutput           = "output"
	FlagTransformer      = "transformer"
	FlagExtractionPolicy = "extraction-policy"
	FlagDecompress       = "decompress"
)

const (
	// ExtractionPolicyAuto is a policy that automatically extracts a resource if it is a recognized archive format.
	// If the resource is not recognized as an archive format, it is downloaded as is.
	ExtractionPolicyAuto = "auto"
	// ExtractionPolicyDisable is a policy that disables extraction of a resource.
	// The resource will not be extracted, even if it is a recognized archive format.
	ExtractionPolicyDisable = "disable"
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

Resources can be accessed either locally or via a plugin that supports remote fetching, with optional credential resolution.

If a resource was stored with compression enabled, it is downloaded in its compressed form by default.
Use --decompress to transparently decompress it. This flag has no effect on resources that are not compressed.`,
		Example: ` # Download a resource with identity 'name=example' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example

  # Download a resource and specify an output file
  ocm download resource ghcr.io/org/component:v1 --identity name=example --output ./my-resource.tar.gz

  # Download a resource and apply a transformer
  ocm download resource ghcr.io/org/component:v1 --identity name=example --transformer my-transformer

  # Download a compressed resource and decompress it
  ocm download resource ghcr.io/org/component:v1 --identity name=example --decompress`,
		RunE:              DownloadResource,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "resource identity to download")
	cmd.Flags().String(FlagOutput, "", "output location to download to. If no transformer is specified, and no "+
		"format was discovered that can be written to a directory, the resource will be written to a file.")
	cmd.Flags().String(FlagTransformer, "", "transformer to use for the output. If not specified, the resource will be written as is. ")
	enum.Var(cmd.Flags(), FlagExtractionPolicy, []string{ExtractionPolicyAuto, ExtractionPolicyDisable},
		"policy to apply when extracting a resource. "+
			"If set to 'disable', the resource will not be extracted, even if they could be. "+
			"If set to 'auto', the resource will be automatically extracted if the returned resource is a recognized archive format.")
	cmd.Flags().Bool(FlagDecompress, false, "decompress the resource if it was stored with compression enabled. "+
		"If the resource is not compressed, this flag has no effect.")

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

	extractionPolicy, err := enum.Get(cmd.Flags(), FlagExtractionPolicy)
	if err != nil {
		return fmt.Errorf("getting extraction policy flag failed: %w", err)
	}

	transformer, err := cmd.Flags().GetString(FlagTransformer)
	if err != nil {
		return fmt.Errorf("getting transformer flag failed: %w", err)
	}

	decompress, err := cmd.Flags().GetBool(FlagDecompress)
	if err != nil {
		return fmt.Errorf("getting decompress flag failed: %w", err)
	}

	requestedIdentity, err := runtime.ParseIdentity(identityStr)
	if err != nil {
		return fmt.Errorf("parsing resource identity %q failed: %w", identityStr, err)
	}

	reference := args[0]
	ref, err := compref.Parse(reference)
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", reference, err)
	}
	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(cmd.Context(), pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, nil, ref)
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

	data, err := shared.DownloadResourceData(cmd.Context(), pluginManager, credentialGraph, ref.Component, ref.Version, repo, res, requestedIdentity)
	if err != nil {
		return fmt.Errorf("downloading resource for identity %q failed: %w", requestedIdentity, err)
	}

	if decompress {
		data, err = compression.Decompress(data)
		if err != nil {
			return fmt.Errorf("decompressing resource failed: %w", err)
		}
	}

	finalOutputPath, err := processResourceOutput(output, res, data, requestedIdentity.String(), logger)
	if err != nil {
		return err
	}

	if transformer != "" {
		availableTransformers := transformers.Transformers()
		transformerConfig, ok := availableTransformers[transformer]
		if !ok {
			return fmt.Errorf("transformer %q not found, available transformers: %v", transformer, slices.Collect(maps.Keys(availableTransformers)))
		}

		plugin, err := pluginManager.BlobTransformerRegistry.GetPlugin(cmd.Context(), transformerConfig)
		if err != nil {
			return fmt.Errorf("getting transformer plugin registered with config under %q failed: %w", transformer, err)
		}

		logger.Info("transforming resource...")
		if data, err = plugin.TransformBlob(cmd.Context(), data, transformerConfig, nil); err != nil {
			return fmt.Errorf("transforming resource failed: %w", err)
		}
		logger.Info("resource transformed successfully")
	}

	defer func() {
		logger.Info("resource downloaded successfully", slog.String("output", finalOutputPath))
	}()

	switch extractionPolicy {
	case ExtractionPolicyAuto:
		extractedFS, err := extractFSFromBlob(data)
		if errors.Is(err, ErrCannotExtractFS) {
			return shared.SaveBlobToFile(data, finalOutputPath)
		}
		if err != nil {
			return err
		}
		return os.CopyFS(finalOutputPath, extractedFS)
	case ExtractionPolicyDisable:
		fallthrough
	default:
		return shared.SaveBlobToFile(data, finalOutputPath)
	}
}

var ErrCannotExtractFS = errors.New("cannot extract resource as filesystem")

func extractFSFromBlob(b blob.ReadOnlyBlob) (_ fs.FS, err error) {
	decompressedOrOriginal, err := compression.Decompress(b)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress resource: %w", err)
	}
	mediaTypeAware, ok := decompressedOrOriginal.(blob.MediaTypeAware)
	if !ok {
		// if were not media type aware, its unsafe to try to extract it, avoid
		return nil, ErrCannotExtractFS
	}

	mediaType, ok := mediaTypeAware.MediaType()
	if !ok {
		return nil, ErrCannotExtractFS
	}

	// TODO(jakobmoellerdev): once we add more compression algorithms, use blob media type for discovery.
	//  For now we just support tar.
	switch {
	case isTar(mediaType):
		data, err := decompressedOrOriginal.ReadCloser()
		if err != nil {
			return nil, fmt.Errorf("failed to read resource: %w", err)
		}
		defer func() {
			err = errors.Join(err, data.Close())
		}()

		return tarfs.New(data)
	default:
		return nil, ErrCannotExtractFS
	}
}

func isTar(mediaType string) bool {
	return slices.Contains([]string{
		"application/tar", "application/x-tar",
	}, mediaType) || strings.HasSuffix(mediaType, "+tar")
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
