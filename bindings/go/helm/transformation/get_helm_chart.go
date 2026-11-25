package transformation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
	"ocm.software/open-component-model/bindings/go/helm/spec/access"
	"ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetHelmChart is a transformer that retrieves Helm charts from remote Helm repositories and buffers them to files.
// It uses the Helm spec specification to determine the repository URL, chart name, version, and any necessary credentials.
// This transformer is designed to support the helm access with classic helm charts.
// For OCI registry access, the OCI registry access transformer should be used instead, which can also handle Helm charts stored in OCI registries.
type GetHelmChart struct {
	Scheme *runtime.Scheme
	// ResourceRepository is used to download helm chart resources and resolve credential consumer identities.
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
	// TempDir is the directory content is buffered in when the spec sets no output path.
	// Empty means the default temporary directory of the operating system.
	TempDir string
}

func (t *GetHelmChart) Transform(ctx context.Context, step runtime.Typed) (_ runtime.Typed, err error) {
	var transformation v1alpha1.GetHelmChart
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get helm transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get helm transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.GetHelmChartOutput{}
	}

	chartOutputPath, err := filesystem.BufferFile(transformation.Spec.OutputPath, t.TempDir, "chart")
	if err != nil {
		return nil, fmt.Errorf("error getting chart output path: %w", err)
	}
	// BufferFile creates the file and writeChartAndProvFiles adds the provenance file next to
	// it; retain both on success for the output, remove them on failure. Failed graph nodes
	// never pass their output to the consuming cleanup node, and the transfer runtime retries
	// transient errors, so each attempt would otherwise leave a file behind.
	defer func() {
		if err != nil {
			for _, path := range []string{chartOutputPath, chartOutputPath + provFileSuffix} {
				if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					slog.WarnContext(ctx, "failed to clean up helm chart output file after error", "path", path, "err", removeErr)
				}
			}
		}
	}()
	slog.DebugContext(ctx, "Going to use chart output path", "path", chartOutputPath)

	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	creds, err := t.resolveCredentials(ctx, targetResource)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Getting helm chart", "resource", transformation.Spec.Resource)

	var helmAccess v1.Helm
	if err := access.Scheme.Convert(transformation.Spec.Resource.Access, &helmAccess); err != nil {
		return nil, fmt.Errorf("failed converting resource spec to Helm: %w", err)
	}

	// Download the chart content via the ResourceRepository.
	// The returned blob is a ChartBlob that provides structured access to the chart archive and prov file.
	downloadedBlob, err := t.ResourceRepository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("error downloading helm chart: %w", err)
	}

	chartBlob := helmblob.NewChartBlob(downloadedBlob)

	chartArchive, err := chartBlob.ChartArchive()
	if err != nil {
		return nil, fmt.Errorf("error extracting chart archive from download: %w", err)
	}

	chartFileSpec, provFileSpec, err := writeChartAndProvFiles(ctx, chartBlob, chartArchive, chartOutputPath)
	if err != nil {
		return nil, err
	}

	name, version, err := readChartMetadata(ctx, chartArchive)
	if err != nil {
		return nil, err
	}

	helmAccess.HelmChart = name
	helmAccess.Version = version
	slog.InfoContext(ctx, "Successfully retrieved helm chart", "chart", helmAccess.HelmChart, "version", helmAccess.Version)

	updatedAccess := runtime.Raw{}
	if err = access.Scheme.Convert(&helmAccess, &updatedAccess); err != nil {
		return nil, fmt.Errorf("failed converting updated v1.Helm access back to resource access format: %w", err)
	}
	targetResource.Access = &updatedAccess

	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	transformation.Output.ChartFile = *chartFileSpec
	transformation.Output.ProvFile = provFileSpec
	transformation.Output.Resource = v2Resource

	return &transformation, nil
}

// resolveCredentials returns credentials for downloading targetResource, or nil if
// no credential provider is configured or the resource has no consumer identity.
// An ErrNotFound from the resolver is treated as "no credentials" rather than an error.
func (t *GetHelmChart) resolveCredentials(ctx context.Context, targetResource *descriptor.Resource) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	consumerId, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed getting resource consumer identity for credential resolution: %w", err)
	}
	if consumerId == nil {
		return nil, nil
	}
	typed, err := t.CredentialProvider.Resolve(ctx, consumerId)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return typed, nil
}

// provFileSuffix is appended to the chart file path to hold the provenance file of a chart.
const provFileSuffix = ".prov"

// writeChartAndProvFiles buffers the chart archive and, if present, the provenance file
// to disk at chartOutputPath and chartOutputPath+provFileSuffix respectively, returning file
// specs for each. The prov file spec is nil when no provenance file is present in the download.
func writeChartAndProvFiles(ctx context.Context, chartBlob *helmblob.ChartBlob, chartArchive blob.ReadOnlyBlob, chartOutputPath string) (*blobv1alpha1.File, *blobv1alpha1.File, error) {
	chartFileSpec, err := filesystem.BlobToSpec(chartArchive, chartOutputPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed buffering chart blob to file: %w", err)
	}
	// A consumer that stores the file as it is records it as a Helm chart archive;
	// ConvertHelmToOCI reads only the URI.
	chartFileSpec.MediaType = registry.ChartLayerMediaType
	slog.DebugContext(ctx, "Converted chart blob to file spec", "uri", chartFileSpec.URI)

	provArchive, err := chartBlob.ProvFile()
	if err != nil {
		return nil, nil, fmt.Errorf("error extracting prov file from download: %w", err)
	}
	if provArchive == nil {
		return chartFileSpec, nil, nil
	}
	provFileSpec, err := filesystem.BlobToSpec(provArchive, chartOutputPath+provFileSuffix)
	if err != nil {
		return nil, nil, fmt.Errorf("failed buffering prov blob to file: %w", err)
	}
	slog.DebugContext(ctx, "Converted prov blob to file spec", "uri", provFileSpec.URI)
	return chartFileSpec, provFileSpec, nil
}

// readChartMetadata loads the Helm chart archive in order to extract the resolved
// chart name and version from its Chart.yaml metadata.
func readChartMetadata(ctx context.Context, chartArchive blob.ReadOnlyBlob) (name, version string, err error) {
	closer, err := chartArchive.ReadCloser()
	if err != nil {
		return "", "", fmt.Errorf("error reading chart archive: %w", err)
	}
	defer func() {
		if err := closer.Close(); err != nil {
			slog.WarnContext(ctx, "error closing chart archive", "error", err)
		}
	}()

	loadedChart, err := loader.LoadArchive(closer)
	if err != nil {
		return "", "", fmt.Errorf("failed loading downloaded chart to read metadata: %w", err)
	}
	return loadedChart.Name(), loadedChart.Metadata.Version, nil
}
