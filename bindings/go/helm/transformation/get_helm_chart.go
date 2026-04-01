package transformation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"helm.sh/helm/v4/pkg/chart/v2/loader"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
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
}

func (t *GetHelmChart) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.GetHelmChart
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get helm transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get helm transformation")
	}
	if t.ResourceRepository == nil {
		return nil, fmt.Errorf("ResourceRepository is required for get helm transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.GetHelmChartOutput{}
	}

	chartOutputPath, err := DetermineOutputPath(transformation.Spec.OutputPath, "chart")
	if err != nil {
		return nil, fmt.Errorf("error getting chart output path: %w", err)
	}
	slog.DebugContext(ctx, "Going to use chart output path", "path", chartOutputPath)

	// Convert resource to internal format
	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	// Resolve credentials if credential provider is available
	var creds map[string]string
	if t.CredentialProvider != nil && t.ResourceRepository != nil {
		if consumerId, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, targetResource); err != nil {
			return nil, fmt.Errorf("failed getting resource consumer identity for credential resolution: %w", err)
		} else if consumerId != nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
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

	chartBlob, ok := downloadedBlob.(*helmblob.ChartBlob)
	if !ok {
		return nil, fmt.Errorf("expected ChartBlob from helm ResourceRepository, got %T", downloadedBlob)
	}

	chartArchive, err := chartBlob.ChartArchive()
	if err != nil {
		return nil, fmt.Errorf("error extracting chart archive from download: %w", err)
	}

	// Convert chart blob to file spec
	chartFileSpec, err := filesystem.BlobToSpec(chartArchive, chartOutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering chart blob to file: %w", err)
	}
	slog.DebugContext(ctx, "Converted chart blob to file spec", "uri", chartFileSpec.URI)

	// Convert prov blob to file spec if it exists
	var provFileSpec *blobv1alpha1.File
	provArchive, err := chartBlob.ProvFile()
	if err != nil {
		return nil, fmt.Errorf("error extracting prov file from download: %w", err)
	}
	if provArchive != nil {
		provSpec, err := filesystem.BlobToSpec(provArchive, fmt.Sprintf("%s.prov", chartOutputPath))
		if err != nil {
			return nil, fmt.Errorf("failed buffering prov blob to file: %w", err)
		}
		slog.DebugContext(ctx, "Converted prov blob to file spec", "uri", provSpec.URI)
		provFileSpec = provSpec
	}

	// Load the chart from the written file to get the resolved name and version
	chartPath := strings.TrimPrefix(chartFileSpec.URI, "file://")
	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed loading downloaded chart to read metadata: %w", err)
	}
	helmAccess.HelmChart = loadedChart.Name()
	helmAccess.Version = loadedChart.Metadata.Version
	slog.InfoContext(ctx, "Successfully retrieved helm chart", "chart", helmAccess.HelmChart, "version", helmAccess.Version)

	updatedAccess := runtime.Raw{}
	if err = access.Scheme.Convert(&helmAccess, &updatedAccess); err != nil {
		return nil, fmt.Errorf("failed converting updated v1.Helm access back to resource access format: %w", err)
	}
	targetResource.Access = &updatedAccess

	// Convert resource to v2 format
	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	// Populate output
	transformation.Output.ChartFile = *chartFileSpec
	transformation.Output.ProvFile = provFileSpec
	transformation.Output.Resource = v2Resource

	return &transformation, nil
}
