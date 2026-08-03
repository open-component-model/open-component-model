package resource

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
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
)

const (
	// ExtractionPolicyAuto is a policy that automatically extracts a resource if it is a recognized archive format.
	// If the resource is not recognized as an archive format, it is downloaded as is.
	ExtractionPolicyAuto = "auto"
	// ExtractionPolicyDisable is a policy that disables extraction of a resource.
	// The resource will not be extracted, even if it is a recognized archive format.
	ExtractionPolicyDisable = "disable"
)

const (
	// LabelDownloadName is the predefined label that defines the default output
	// filename for `download resource`. It follows the OCM label naming convention
	// (DNS-prefixed name with a kebab-case local part).
	LabelDownloadName = "ocm.software/download-name"
	// LabelDownloadNameLegacy is the deprecated flat camelCase form of LabelDownloadName.
	//
	// Deprecated: use LabelDownloadName. The legacy name is still honored but will be
	// removed after one to two releases.
	LabelDownloadNameLegacy = "downloadName"
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

The output filename is determined by the first of these that applies:
  1. --output, if explicitly provided
  2. An "ocm.software/download-name" label on the resource, if present
     (the legacy "downloadName" label is still honored but deprecated)
  3. The resource name, with its extra identity attributes appended when present`,
		Example: ` # Download a resource with identity 'name=example' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example

  # Download a resource with identity 'name=example' and 'architecture=amd64' and write to default output
  ocm download resource ghcr.io/org/component:v1 --identity name=example,architecture=amd64

  # Download a resource and specify an output file
  ocm download resource ghcr.io/org/component:v1 --identity name=example --output ./my-resource.tar.gz

  # Download a resource and apply a transformer
  ocm download resource ghcr.io/org/component:v1 --identity name=example --transformer my-transformer`,
		RunE:              DownloadResource,
		DisableAutoGenTag: true,
	}

	cmd.Flags().String(FlagResourceIdentity, "", "resource identity to download")
	cmd.Flags().String(FlagOutput, "", "full output file path (directory + filename). Intermediate directories are created automatically. "+
		"Takes precedence over an ocm.software/download-name label on the resource.")
	cmd.Flags().String(FlagTransformer, "", "transformer to use for the output. If not specified, the resource will be written as is. ")
	enum.Var(cmd.Flags(), FlagExtractionPolicy, []string{ExtractionPolicyAuto, ExtractionPolicyDisable},
		"policy to apply when extracting a resource. "+
			"If set to 'disable', the resource will not be extracted, even if they could be. "+
			"If set to 'auto', the resource will be automatically extracted if the returned resource is a recognized archive format.")

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
		logger.Info("using explicit --output", slog.String("output", output))
		return output, nil
	}

	// download-name label: default filename chosen by the component author. Prefer the
	// conventional name; the legacy name is still honored (with a warning) for now.
	var label *descriptor.Label
	for i := range resource.Labels {
		switch resource.Labels[i].Name {
		case LabelDownloadName:
			label = &resource.Labels[i]
		case LabelDownloadNameLegacy:
			if label == nil {
				label = &resource.Labels[i]
			}
		}
	}
	if label != nil {
		if label.Name == LabelDownloadNameLegacy {
			logger.Warn("resource uses a deprecated label name",
				slog.String("deprecated", LabelDownloadNameLegacy), slog.String("use", LabelDownloadName))
		}
		var downloadName string
		if err := label.GetValue(&downloadName); err != nil {
			return "", fmt.Errorf("interpreting %q label value failed: %w", label.Name, err)
		}
		if downloadName = filepath.Clean(downloadName); filepath.IsAbs(downloadName) || downloadName == ".." || strings.HasPrefix(downloadName, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%q label value %q must be a relative path within the output directory", label.Name, downloadName)
		}
		logger.Info("using download-name label for output filename", slog.String("output", downloadName))
		return downloadName, nil
	}

	// Fallback: resource name, with extra identity attributes appended when present.
	output = fallbackFileName(resource)
	logger.Warn("no output location specified, deriving output file name from the resource", slog.String("output", output))

	return output, nil
}

// fallbackFileName derives a filesystem-safe filename from the resource name plus its
// extra identity attribute values (sorted by key), so variants of the same name stay distinct.
func fallbackFileName(resource *descriptor.Resource) string {
	name := sanitizeFileName(resource.Name)
	for _, key := range slices.Sorted(maps.Keys(resource.ExtraIdentity)) {
		name += "-" + sanitizeFileName(resource.ExtraIdentity[key])
	}
	return name
}

// sanitizeFileName maps characters that are unsafe in a filename to '-'. Extra identity
// values are unconstrained, so this stops a value such as "windows/amd64" from injecting a
// path separator into the fallback name, which is written relative to the working directory.
func sanitizeFileName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, s)
}
