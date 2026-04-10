package internal

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	helmtransformer "ocm.software/open-component-model/bindings/go/helm/transformation"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	ocitransformer "ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
)

// NewDefaultBuilder creates a builder.Builder pre-configured with all standard OCI, CTF, and Helm transformers.
// It accepts the repository provider, resource repository, and credential resolver interfaces
// that are needed by the transformers to interact with repositories.
func NewDefaultBuilder(
	repoProvider repository.ComponentVersionRepositoryProvider,
	resourceRepo repository.ResourceRepository,
	credentialProvider credentials.Resolver,
) *builder.Builder {
	transformerScheme := runtime.NewScheme()
	transformerScheme.MustRegisterScheme(ociv1alpha1.Scheme)
	transformerScheme.MustRegisterScheme(ociaccess.Scheme)
	transformerScheme.MustRegisterScheme(helmv1alpha1.Scheme)

	ociGet := &ocitransformer.GetComponentVersion{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}
	ociAdd := &ocitransformer.AddComponentVersion{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}

	// Resource transformers
	ociGetResource := &ocitransformer.GetLocalResource{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}
	ociAddResource := &ocitransformer.AddLocalResource{
		Scheme:             transformerScheme,
		RepoProvider:       repoProvider,
		CredentialProvider: credentialProvider,
	}

	// OCI Artifact transformers
	ociGetOCIArtifact := &ocitransformer.GetOCIArtifact{
		Scheme:             transformerScheme,
		Repository:         resourceRepo,
		CredentialProvider: credentialProvider,
	}

	ociAddOCIArtifact := &ocitransformer.AddOCIArtifact{
		Scheme:             transformerScheme,
		Repository:         resourceRepo,
		CredentialProvider: credentialProvider,
	}

	// Helm transformers
	getHelmChart := &helmtransformer.GetHelmChart{
		Scheme:                           transformerScheme,
		ResourceConsumerIdentityProvider: &helmaccess.HelmAccess{},
		CredentialProvider:               credentialProvider,
	}
	convertHelmToOCI := &helmtransformer.ConvertHelmChartToOCI{
		Scheme: transformerScheme,
	}

	// File cleanup transformer
	transformerScheme.MustRegisterWithAlias(&FileCleanupTransformation{}, FileCleanupVersionedType)
	fileCleanup := &FileCleanup{
		Scheme: transformerScheme,
	}

	return builder.NewBuilder(transformerScheme).
		WithTransformer(&ociv1alpha1.OCIGetComponentVersion{}, ociGet).
		WithTransformer(&ociv1alpha1.OCIAddComponentVersion{}, ociAdd).
		WithTransformer(&ociv1alpha1.CTFGetComponentVersion{}, ociGet).
		WithTransformer(&ociv1alpha1.CTFAddComponentVersion{}, ociAdd).
		WithTransformer(&ociv1alpha1.OCIGetLocalResource{}, ociGetResource).
		WithTransformer(&ociv1alpha1.OCIAddLocalResource{}, ociAddResource).
		WithTransformer(&ociv1alpha1.CTFGetLocalResource{}, ociGetResource).
		WithTransformer(&ociv1alpha1.CTFAddLocalResource{}, ociAddResource).
		WithTransformer(&ociv1alpha1.GetOCIArtifact{}, ociGetOCIArtifact).
		WithTransformer(&ociv1alpha1.AddOCIArtifact{}, ociAddOCIArtifact).
		WithTransformer(&helmv1alpha1.GetHelmChart{}, getHelmChart).
		WithTransformer(&helmv1alpha1.ConvertHelmToOCI{}, convertHelmToOCI).
		WithTransformer(&FileCleanupTransformation{}, fileCleanup)
}
