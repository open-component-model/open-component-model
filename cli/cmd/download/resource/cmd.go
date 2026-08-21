package resource

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/nlepage/go-tarfs"
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
	"ocm.software/open-component-model/cli/internal/transformers"
)

const (
	FlagResourceIdentity = "identity"
	FlagOutput           = "output"
	FlagTransformer      = "transformer"
	FlagExtractionPolicy = "extraction-policy"
	FlagSBOM             = "sbom"
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

If no transformer is specified, the resource is written directly in its original format.

Resources can be accessed either locally or via a plugin that supports remote fetching, with optional credential resolution.

With --sbom, the Software Bills of Materials describing the resource are downloaded instead of the
resource itself. This is EXPERIMENTAL: what is discovered, how it is written out and the flags
themselves may change in a future release. They are looked for in two ways, in order:

  1. Another resource of the same component version declaring, through the
     "ocm.software/artefact-references" label, that it describes the selected resource.
  2. For a resource backed by an OCI artifact, SBOMs attached to that artifact by
     "docker buildx build --sbom=true". SBOMs attached by other tooling, such as cosign
     or the OCI referrers API, are not discovered.

Every SBOM found is written to its own file in a directory, byte for byte as published, so digests
and signatures over them still apply. The directory is --output, or the values of the resource
identity joined by "-" when that is not given, so --identity name=image,architecture=amd64 writes
into "image-amd64". The paths written are printed to standard output, one per line.

When --output is not provided, the output filename is the resource name.`,
		Example: ` # Download a resource with identity 'name=example' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example

  # Download a resource with identity 'name=example' and 'architecture=amd64' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example,architecture=amd64

  # Download a resource and specify an output file
  ocm download resource ghcr.io/org/component:v1 --identity name=example --output ./my-resource.tar.gz

  # Download a resource and apply a transformer
  ocm download resource ghcr.io/org/component:v1 --identity name=example --transformer my-transformer

  # Download every SBOM describing a resource into a directory
  ocm download resource ghcr.io/org/component:v1 --identity name=example --sbom --output ./sboms

  # Scan every SBOM found for a resource
  ocm download resource ghcr.io/org/component:v1 --identity name=example --sbom | xargs -n1 grype sbom:`,
		RunE:              DownloadResource,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "resource identity to download")
	cmd.Flags().String(FlagOutput, "", "output location to download to. If no transformer is specified, and no "+
		"format was discovered that can be written to a directory, the resource will be written to a file. "+
		"With --sbom this is the directory the SBOMs are written into, defaulting to the values of the "+
		"resource identity joined by \"-\".")
	cmd.Flags().String(FlagTransformer, "", "transformer to use for the output. If not specified, the resource will be written as is. ")
	cmd.Flags().Bool(FlagSBOM, false, "experimental: download the SBOMs describing the resource instead of the "+
		"resource itself, writing every SBOM found to its own file in the output directory. What is discovered, "+
		"how it is written out and this flag itself may change in a future release")
	enum.Var(cmd.Flags(), FlagExtractionPolicy, []string{ExtractionPolicyAuto, ExtractionPolicyDisable},
		"policy to apply when extracting a resource. "+
			"If set to 'disable', the resource will not be extracted, even if they could be. "+
			"If set to 'auto', the resource will be automatically extracted if the returned resource is a recognized archive format.")

	cmd.MarkFlagsMutuallyExclusive(FlagSBOM, FlagTransformer)
	cmd.MarkFlagsMutuallyExclusive(FlagSBOM, FlagExtractionPolicy)

	return cmd
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

	wantSBOM, err := cmd.Flags().GetBool(FlagSBOM)
	if err != nil {
		return fmt.Errorf("getting sbom flag failed: %w", err)
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

	if wantSBOM {
		return downloadSBOMs(cmd, downloadContext{
			pluginManager:   pluginManager,
			credentialGraph: credentialGraph,
			logger:          logger,
			ref:             ref,
			repo:            repo,
			descriptor:      desc,
			resource:        res,
			output:          output,
			identity:        requestedIdentity,
		})
	}

	data, err := shared.DownloadResourceData(cmd.Context(), pluginManager, credentialGraph, ref.Component, ref.Version, repo, res, requestedIdentity)
	if err != nil {
		return fmt.Errorf("downloading resource for identity %q failed: %w", requestedIdentity, err)
	}

	finalOutputPath, err := processResourceOutput(output, res, logger)
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
		// decompress in any case - DecompressedBlob lazily decompresses or returns the original blob based on the media type
		decompressedOrOriginal, err := compression.Decompress(data)
		if err != nil {
			return fmt.Errorf("decompressing resource failed: %w", err)
		}

		// try extracting FS
		extractedFS, err := extractFSFromBlob(decompressedOrOriginal)
		if err != nil && !errors.Is(err, ErrCannotExtractFS) {
			return fmt.Errorf("extracting resource as filesystem failed: %w", err)
		}

		if extractedFS != nil {
			return os.CopyFS(finalOutputPath, extractedFS)
		}

		// if we cannot extract a fs, since it's not supported, return the decompressed blob
		return shared.SaveBlobToFile(decompressedOrOriginal, finalOutputPath)
	case ExtractionPolicyDisable:
		fallthrough
	default:
		return shared.SaveBlobToFile(data, finalOutputPath)
	}
}

var ErrCannotExtractFS = errors.New("cannot extract resource as filesystem")

func extractFSFromBlob(b blob.ReadOnlyBlob) (_ fs.FS, err error) {
	mediaTypeAware, ok := b.(blob.MediaTypeAware)
	if !ok {
		// if were not media type aware, it's unsafe to try to extract it, avoid
		return nil, fmt.Errorf("blob is not media type aware: %w", ErrCannotExtractFS)
	}

	mediaType, ok := mediaTypeAware.MediaType()
	if !ok {
		return nil, ErrCannotExtractFS
	}

	// TODO(jakobmoellerdev): once we add more compression algorithms, use blob media type for discovery.
	//  For now we just support tar.
	switch {
	case isTar(mediaType):
		data, err := b.ReadCloser()
		if err != nil {
			return nil, fmt.Errorf("failed to read resource: %w", err)
		}
		defer func() {
			err = errors.Join(err, data.Close())
		}()

		f, err := tarfs.New(data)
		return f, err
	default:
		return nil, ErrCannotExtractFS
	}
}

func isTar(mediaType string) bool {
	return slices.Contains([]string{
		"application/tar", "application/x-tar",
	}, mediaType) || strings.HasSuffix(mediaType, "+tar")
}

func processResourceOutput(output string, resource *descriptor.Resource, logger *slog.Logger) (string, error) {
	if output != "" {
		logger.Debug("using explicit --output", slog.String("output", output))
		return output, nil
	}

	// Fallback: resource name.
	output = resource.Name
	logger.Debug("no output location specified, using resource name as output file name", slog.String("output", output))

	return output, nil
}
