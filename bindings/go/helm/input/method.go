package input

import (
	"context"
	"fmt"
	"path"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrLocalHelmInputDoesNotRequireCredentials is returned when credential-related operations are attempted
// on local helm inputs, since those are based on local filesystem and do not require authentication or authorization.
var ErrLocalHelmInputDoesNotRequireCredentials = fmt.Errorf("local helm inputs do not require credentials")

var _ interface {
	constructor.ResourceInputMethod
} = (*InputMethod)(nil)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.Helm{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}

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

// GetResourceCredentialConsumerIdentity returns credentials consumer identity for remote helm repositories
// or nil for local helm inputs. Remote repositories may require authentication credentials.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	helm := v1.Helm{}
	if err := Scheme.Convert(resource.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if helm.HelmRepository == "" {
		return nil, ErrLocalHelmInputDoesNotRequireCredentials
	}

	identity, err = runtime.ParseURLToIdentity(helm.HelmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	identity.SetType(runtime.NewVersionedType(v1.Type, v1.Version))

	return identity, nil
}

// ProcessResource processes a helm-based resource input by converting the input specification
// to a v1.Helm format, reading from local filesystem or downloading from remote repository,
// and returning both the processed blob data and resource access information.
//
// For local charts (a path specified): Returns only ProcessedBlobData (local access)
// For remote charts (helmRepository specified): Returns both ProcessedResource (remote access) and ProcessedBlobData
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	helm := v1.Helm{}
	if err := Scheme.Convert(resource.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	helmBlob, err := GetV1HelmBlobWithCredentials(ctx, helm, i.TempFolder, credentials)
	if err != nil {
		return nil, fmt.Errorf("error getting helm blob based on resource input specification: %w", err)
	}

	result = &constructor.ResourceInputMethodResult{
		ProcessedBlobData: helmBlob,
	}

	// if the path is not set, create remote resource access
	if helm.HelmRepository != "" && helm.Path == "" {
		remoteResource, err := i.createRemoteResourceAccess(helm)
		if err != nil {
			return nil, fmt.Errorf("error creating remote resource access: %w", err)
		}
		result.ProcessedResource = remoteResource
	}

	return result, nil
}

// createRemoteResourceAccess creates a resource descriptor with remote access information
// for helm charts stored in remote repositories.
func (i *InputMethod) createRemoteResourceAccess(helm v1.Helm) (*descriptorruntime.Resource, error) {
	ociAccess := &ocispec.OCIImage{
		Type: runtime.Type{
			Name: ocispec.LegacyType3, // TODO: is this correct?
		},
		ImageReference: helm.HelmRepository,
	}

	// TODO: Support version hints? Maybe the helmRepository should be a URL with version?
	if helm.Version != "" {
		ociAccess.ImageReference = helm.HelmRepository + ":" + helm.Version
	}

	// TODO: Very dumb override if repository hint is set. Is this enough?
	if helm.Repository != "" {
		ociAccess.ImageReference = helm.Repository
		if helm.Version != "" {
			ociAccess.ImageReference = helm.Repository + ":" + helm.Version
		}
	}

	// TODO: How do we create these? :D
	resource := &descriptorruntime.Resource{
		ElementMeta: descriptorruntime.ElementMeta{
			ObjectMeta: descriptorruntime.ObjectMeta{
				Name:    extractChartNameFromPath(helm.HelmRepository),
				Version: helm.Version,
			},
		},
		Type:     HelmRepositoryType,
		Relation: descriptorruntime.ExternalRelation,
		Access:   ociAccess,
	}

	return resource, nil
}

// extractChartNameFromPath extracts the chart name from the path or returns a default name
func extractChartNameFromPath(ref string) string {
	// TODO: for now, use the last part of the path as chart name
	return path.Base(ref)
}
