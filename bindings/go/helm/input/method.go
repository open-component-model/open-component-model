package input

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	helminternal "ocm.software/open-component-model/bindings/go/helm/internal"
	credsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/helm/spec/input"
	"ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ interface {
	constructor.ResourceInputMethod
} = (*InputMethod)(nil)

// InputMethod implements the ResourceInputMethod and SourceInputMethod interfaces for helm-based inputs.
// It provides functionality to process local filesystem directories, which have helm chart structure,
// as either resources or sources in the OCM constructor system.
//
// Since directories are accessed directly from the local filesystem, no credentials
// are required for any operations.
//
// The TempFolder field is used to specify a base temporary folder for processing helm charts.
// It is set by the user when creating an instance of InputMethod. If the field is empty,
// the system's default temporary directory will be used.
type InputMethod struct {
	TempFolder string
}

// LegacyHelmChartConsumerType is the type of the identity for remote helm repositories.
const LegacyHelmChartConsumerType = "HelmChartRepository"

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity returns credentials consumer identity for remote helm repositories
// or [ErrLocalHelmInputDoesNotRequireCredentials] for local helm inputs.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	helm := v1.Helm{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if helm.HelmRepository == "" {
		slog.DebugContext(ctx, "no credentials are needed for local helm charts")
		return nil, nil
	}

	return helminternal.CredentialConsumerIdentity(helm.HelmRepository)
}

// ProcessResource processes a helm-based resource input by converting the input specification
// to a v1.Helm format, reading from local filesystem or downloading from remote repository,
// and returning both the processed blob data and resource access information.
//
// For local charts (a path specified): Returns only ProcessedBlobData (local access)
// For remote charts (helmRepository specified): Returns both ProcessedResource (remote access) and ProcessedBlobData
//
// TODO(matthiasbruns): migrate credentials parameter to runtime.Typed once the ResourceInputMethod interface is updated.
// https://github.com/open-component-model/ocm-project/issues/988
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	helm := v1.Helm{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if i.TempFolder == "" {
		// we cannot delete the temp folder since it will hold the downloaded helm chart blobs
		// and we do not want to break existing fallback behavior for users who do not set the TempFolder field before
		temp, err := os.MkdirTemp("", "helm-input-*")
		if err != nil {
			return nil, fmt.Errorf("error creating temporary directory for helm input processing: %w", err)
		}
		i.TempFolder = temp
	}

	helmBlob, chart, err := GetV1HelmBlob(ctx, helm, i.TempFolder,
		WithCredentials(credsv1.FromDirectCredentials(credentials)),
		WithOCICredentials(ocicredsv1.FromDirectCredentials(credentials)),
	)
	if err != nil {
		return nil, fmt.Errorf("error getting helm blob based on resource input specification: %w", err)
	}

	result = &constructor.ResourceInputMethodResult{
		ProcessedBlobData: helmBlob,
	}

	if helm.Repository != "" {
		remoteResource, err := i.createRemoteResourceAccess(resource, helm, chart)
		if err != nil {
			return nil, fmt.Errorf("error creating remote resource access: %w", err)
		}

		res := constructorruntime.ConvertToDescriptorResource(remoteResource)
		result.ProcessedResource = res
	}

	return result, nil
}

// createRemoteResourceAccess creates a resource descriptor with remote access information
// for helm charts stored in remote repositories.
func (i *InputMethod) createRemoteResourceAccess(resource *constructorruntime.Resource, helm v1.Helm, chart *ReadOnlyChart) (*constructorruntime.Resource, error) {
	ref, err := looseref.ParseReference(helm.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed to parse target access image reference %q: %w", helm.Repository, err)
	}

	if ref.Tag == "" {
		return nil, fmt.Errorf("tag is required for remote helm repository")
	}

	// If the given repository oci reference contains a tag, make sure it matches the version derived
	// from the fetched chart.
	if ref.Tag != chart.Version {
		return nil, fmt.Errorf("provided version %q does not match tag %q", ref.Tag, chart.Version)
	}

	ociAccess := &ocispec.OCIImage{
		ImageReference: ref.String(),
	}

	// set the default type for OCIImage
	if _, err := access.Scheme.DefaultType(ociAccess); err != nil {
		return nil, fmt.Errorf("error setting default type for OCIImage: %w", err)
	}

	resource.Access = ociAccess
	resource.Type = HelmRepositoryType

	return resource, nil
}
