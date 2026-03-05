package transformation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm"
	"ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	"ocm.software/open-component-model/bindings/go/helm/oci"
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

	var (
		chartSpec *filesystem.Blob
		provSpec  *filesystem.Blob
	)

	fs, err := filesystem.NewFS(dirFromURI(transformation.Spec.ChartFile.URI), os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("failed creating filesystem from chart file spec: %w", err)
	}
	chartSpec = filesystem.NewFileBlob(fs, fileFromURI(transformation.Spec.ChartFile.URI))

	if transformation.Spec.ProvFile != nil {
		fs, err := filesystem.NewFS(dirFromURI(transformation.Spec.ProvFile.URI), os.O_RDONLY)
		if err != nil {
			return nil, fmt.Errorf("failed creating filesystem from prov file spec: %w", err)
		}
		provSpec = filesystem.NewFileBlob(fs, fileFromURI(transformation.Spec.ProvFile.URI))
	}

	outputPath, err := DetermineOutputPath(transformation.Spec.OutputPath, "oci")
	if err != nil {
		return nil, fmt.Errorf("error getting OCI output path: %w", err)
	}

	result := oci.CopyChartToOCILayout(ctx, &helm.ChartData{
		Name:      helmAccess.GetChartName(),
		Version:   helmAccess.GetVersion(),
		ChartBlob: chartSpec,
		ProvBlob:  provSpec,
	})

	// BlobToSpec fully consumes the blob (copies to disk), which allows the
	// background goroutine to finish and makes the descriptor available.
	spec, err := filesystem.BlobToSpec(result, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed converting OCI layout blob to file spec: %w", err)
	}

	// Now that the blob is consumed, we can safely retrieve the descriptor.
	ociDesc, err := result.Descriptor(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get OCI manifest descriptor: %w", err)
	}

	transformation.Output.File = *spec
	transformation.Output.Resource = transformation.Spec.Resource
	// Update output.Resource.Type to ociImage
	transformation.Output.Resource.Type = "ociImage"

	// Use digest of top-level-manifest
	transformation.Output.Resource.Digest = &descv2.Digest{
		HashAlgorithm:          string(ociDesc.Digest.Algorithm()),
		NormalisationAlgorithm: descv2.ExcludeFromSignature,
		Value:                  ociDesc.Digest.Encoded(),
	}

	reference, err := referenceFromHelmAccess(helmAccess)
	if err != nil {
		return nil, fmt.Errorf("failed constructing OCI image reference from Helm access: %w", err)
	}

	ociAccess := ociv2.OCIImage{
		Type: runtime.Type{
			Version: ociv2.LegacyTypeVersion,
			Name:    ociv2.LegacyType,
		},
		ImageReference: reference,
	}
	updatedAccess := runtime.Raw{}
	if err = ociaccess.Scheme.Convert(&ociAccess, &updatedAccess); err != nil {
		return nil, fmt.Errorf("failed converting ociv2.OCIImage access back to resource access format: %w", err)
	}
	transformation.Output.Resource.Relation = descv2.LocalRelation
	transformation.Output.Resource.Access = &updatedAccess

	return &transformation, nil
}

func referenceFromHelmAccess(helmAccess v1.Helm) (string, error) {
	// if HelmChart is provided separately in the spec, append it to the repository URL to get the full URL for the chart.
	parts := []string{helmAccess.HelmRepository, helmAccess.GetChartName()}
	ref := strings.Join(parts, "/")
	if helmAccess.GetVersion() != "" {
		ref = ref + ":" + helmAccess.GetVersion()
	}

	lref, err := looseref.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("failed parsing reference from Helm access: %w", err)
	}

	if lref.Repository == "" {
		return "", fmt.Errorf("repository is required in Helm access to construct OCI image reference")
	}

	ref = lref.Repository
	if lref.Tag != "" {
		ref = ref + ":" + lref.Tag
	}

	return ref, nil
}

func dirFromURI(uri string) string {
	uri = strings.TrimPrefix(uri, "file://")
	// only return path to dir, not including the file name
	return filepath.Dir(uri)
}

func fileFromURI(uri string) string {
	uri = strings.TrimPrefix(uri, "file://")
	// only return file name, not including the path to the dir
	return filepath.Base(uri)
}
