package transformation

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm"
	"ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	"ocm.software/open-component-model/bindings/go/helm/internal/oci"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociv2 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ConvertHelmChartToOCI struct {
	Scheme *runtime.Scheme
}

func (t *ConvertHelmChartToOCI) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.ConvertHelmToOCI
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to convert helm transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for convert helm transformation")
	}
	if transformation.Spec.Resource == nil || transformation.Spec.Resource.Access == nil {
		return nil, fmt.Errorf("spec.resource and spec.resource.access are required for convert helm transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.ConvertHelmToOCIOutput{}
	}

	var helmAccess v1.Helm
	if err := access.Scheme.Convert(transformation.Spec.Resource.Access, &helmAccess); err != nil {
		return nil, fmt.Errorf("failed converting resource spec to v1.Helm: %w", err)
	}

	if transformation.Spec.ChartFile.URI == "" {
		return nil, fmt.Errorf("spec.chartFile.uri is required for convert helm transformation")
	}

	var (
		chartSpec *filesystem.Blob
		provSpec  *filesystem.Blob
	)

	chartPath := pathFromURI(transformation.Spec.ChartFile.URI)
	fs, err := filesystem.NewFS(filepath.Dir(chartPath), os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("failed creating filesystem from chart file spec: %w", err)
	}
	slog.DebugContext(ctx, "Created filesystem for chart file", "uri", transformation.Spec.ChartFile.URI)
	chartSpec = filesystem.NewFileBlob(fs, filepath.Base(chartPath))

	if transformation.Spec.ProvFile != nil && transformation.Spec.ProvFile.URI != "" {
		provPath := pathFromURI(transformation.Spec.ProvFile.URI)
		fs, err := filesystem.NewFS(filepath.Dir(provPath), os.O_RDONLY)
		if err != nil {
			return nil, fmt.Errorf("failed creating filesystem from prov file spec: %w", err)
		}
		slog.DebugContext(ctx, "Created filesystem for prov file", "uri", transformation.Spec.ProvFile.URI)
		provSpec = filesystem.NewFileBlob(fs, filepath.Base(provPath))
	}

	outputPath, err := DetermineOutputPath(transformation.Spec.OutputPath, "oci")
	if err != nil {
		return nil, fmt.Errorf("error getting OCI output path: %w", err)
	}
	slog.DebugContext(ctx, "Going to use oci output path", "path", outputPath)

	result, err := oci.CopyChartToOCILayout(ctx, &helm.ChartData{
		Name:      helmAccess.GetChartName(),
		Version:   helmAccess.GetVersion(),
		ChartBlob: chartSpec,
		ProvBlob:  provSpec,
	}, transformation.Spec.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI layout from helm chart: %w", err)
	}

	if result.Blob == nil {
		return nil, fmt.Errorf("OCI layout blob is required but was not returned from OCI layout creation")
	}
	if result.Desc == nil {
		return nil, fmt.Errorf("OCI layout descriptor is required but was not returned from OCI layout creation")
	}

	spec, err := filesystem.BlobToSpec(result.Blob, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed converting OCI layout blob to file spec: %w", err)
	}

	slog.InfoContext(ctx, "Got OCI manifest descriptor")

	transformation.Output.File = *spec
	transformation.Output.Resource = transformation.Spec.Resource
	transformation.Output.Resource.Type = "helmChart"

	// Use digest of top-level-manifest
	transformation.Output.Resource.Digest = &descv2.Digest{
		HashAlgorithm: string(result.Desc.Digest.Algorithm()),
		Value:         result.Desc.Digest.Encoded(),
	}
	slog.DebugContext(ctx, "Set OCI image imageReference digest in output resource", "value", transformation.Output.Resource.Digest.Value)

	imageReference, err := imageReferenceFromHelmAccess(helmAccess)
	if err != nil {
		return nil, fmt.Errorf("failed constructing OCI image imageReference from Helm access: %w", err)
	}
	slog.InfoContext(ctx, "Constructed OCI image imageReference from Helm access", "imageReference", imageReference)

	ociAccess := ociv2.OCIImage{
		Type: runtime.Type{
			Version: ociv2.LegacyTypeVersion,
			Name:    ociv2.LegacyType,
		},
		ImageReference: imageReference,
	}
	updatedAccess := runtime.Raw{}
	if err = ociaccess.Scheme.Convert(&ociAccess, &updatedAccess); err != nil {
		return nil, fmt.Errorf("failed converting OCIImage access back to resource access format: %w", err)
	}
	transformation.Output.Resource.Access = &updatedAccess

	return &transformation, nil
}

func imageReferenceFromHelmAccess(helmAccess v1.Helm) (string, error) {
	reference, err := helmAccess.ChartReference()
	if err != nil {
		return "", fmt.Errorf("failed getting chart reference from Helm access: %w", err)
	}
	lref, err := looseref.ParseReference(reference)
	if err != nil {
		return "", fmt.Errorf("failed parsing reference from Helm access: %w", err)
	}

	if lref.Repository == "" {
		return "", fmt.Errorf("repository is required in Helm access to construct OCI image reference")
	}

	// Use only the repository path (without registry/domain) as the OCI image reference.
	ref := lref.Repository
	if lref.Tag != "" {
		ref = ref + ":" + lref.Tag
	}

	return ref, nil
}

func pathFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return u.Path
}
