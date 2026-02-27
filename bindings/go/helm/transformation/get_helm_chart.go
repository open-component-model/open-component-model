package transformation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm"
	"ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	"ocm.software/open-component-model/bindings/go/helm/download"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetHelmChart is a transformer that retrieves Helm charts from remote Helm repositories and buffers them to files.
// It uses the Helm spec specification to determine the repository URL, chart name, version, and any necessary credentials.
// This transformer is designed to support the helm access with classic helm charts.
// For OCI registry access, the OCI registry access transformer should be used instead, which can also handle Helm charts stored in OCI registries.
type GetHelmChart struct {
	Scheme *runtime.Scheme
	// ResourceConsumerIdentityProvider is used to get the consumer identity for the resource when resolving credentials.
	ResourceConsumerIdentityProvider helm.ResourceConsumerIdentityProvider
	CredentialProvider               credentials.Resolver
}

func (t *GetHelmChart) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.GetHelmChart
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get helm transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get helm transformation")
	}

	var output *v1alpha1.GetHelmChartOutput
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.GetHelmChartOutput{}
	}
	output = transformation.Output

	chartOutputPath, err := DetermineOutputPath(transformation.Spec.OutputPath, "chart")
	if err != nil {
		return nil, fmt.Errorf("error getting chart output path: %w", err)
	}

	// Convert resource to internal format
	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	// Resolve credentials if credential provider is available
	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.ResourceConsumerIdentityProvider.GetResourceCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	slog.Info("Getting helm chart", "resource", transformation.Spec.Resource)

	var helmAccess v1.Helm
	if err := access.Scheme.Convert(transformation.Spec.Resource.Access, &helmAccess); err != nil {
		return nil, fmt.Errorf("failed converting resource spec to v1.Helm: %w", err)
	}

	// Configure the downloader options based on the Helm access specification and resolved credentials
	// We omit backwards compatibility options like certificates and keyrings and rely on the credentials approach.
	opts := []download.Option{
		// If a version is specified in the Helm access spec, use it to ensure we get the correct chart version.
		// If not specified, the downloader will use the default behavior which tries to get the version from helmAccess.HelmRepository
		download.WithVersion(helmAccess.Version),
		download.WithTempDirBase(transformation.Spec.OutputPath),
		download.WithCredentials(creds),
		// Override the default downloader behavior to always download the chart and prov files.
		download.WithAlwaysDownloadProv(true),
	}

	helmURL := helmAccess.HelmRepository
	if helmAccess.HelmChart != "" {
		// if HelmChart is provided separately in the spec, append it to the repository URL to get the full URL for the chart.
		helmURL = fmt.Sprintf("%s/%s", helmURL, helmAccess.HelmChart)
	}

	// TODO(matthiasbruns): Introduce a helm based ResourceRepository and handle access in there https://github.com/open-component-model/ocm-project/issues/911
	resultData, err := download.NewReadOnlyChartFromRemote(ctx, helmURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading helm chart from repository %q: %w", helmURL, err)
	}

	// Convert chart blob to file spec
	chartFileSpec, err := filesystem.BlobToSpec(resultData.ChartBlob, chartOutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering chart blob to file: %w", err)
	}

	// Convert prov blob to file spec if it exists
	var provFileSpec *blobv1alpha1.File
	if resultData.ProvBlob != nil {
		provSpec, err := filesystem.BlobToSpec(resultData.ProvBlob, fmt.Sprintf("%s.prov", chartOutputPath))
		if err != nil {
			return nil, fmt.Errorf("failed buffering prov blob to file: %w", err)
		}
		provFileSpec = provSpec
	}

	// Update access information with the retrieved chart data
	helmAccess.HelmChart = resultData.Name
	helmAccess.Version = resultData.Version

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
	output.ChartFile = *chartFileSpec
	output.ProvFile = provFileSpec
	output.Resource = v2Resource

	return &transformation, nil
}
