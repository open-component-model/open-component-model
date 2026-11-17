package internal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocitransformer "ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// TransferToLocalBlob is a fused transformer that streams a plain blob from a Wget or S3
// access into a component version in an OCI or CTF repository as a local blob. The blob
// read from the source is handed to the target repository, which decides whether it needs
// to be buffered. It handles the Transfer{Wget,S3}To{OCI,CTF}LocalBlob transformations.
type TransferToLocalBlob struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

// isRepeatableWgetVerb reports whether a Wget request with verb can be streamed: the
// content may be requested again (retries, repeated reads), which only GET allows safely.
func isRepeatableWgetVerb(verb string) bool {
	return verb == "" || strings.EqualFold(verb, http.MethodGet)
}

func (t *TransferToLocalBlob) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating transfer to local blob transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer to local blob transformation: %w", err)
	}

	var (
		access             runtime.Typed
		target             runtime.Typed
		globalAccessPolicy ocirepospecv1.GlobalAccessPolicy
		component, version string
		targetResource     *v2.Resource
		output             **uploadv1alpha1.TransferToLocalBlobOutput
	)
	switch tr := transformation.(type) {
	case *uploadv1alpha1.TransferWgetToOCILocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		access, target, globalAccessPolicy = &tr.Spec.Access, &tr.Spec.Repository, tr.Spec.GlobalAccessPolicy
		component, version, targetResource, output = tr.Spec.Component, tr.Spec.Version, tr.Spec.TargetResource, &tr.Output
	case *uploadv1alpha1.TransferWgetToCTFLocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		access, target = &tr.Spec.Access, &tr.Spec.Repository
		component, version, targetResource, output = tr.Spec.Component, tr.Spec.Version, tr.Spec.TargetResource, &tr.Output
	case *uploadv1alpha1.TransferS3ToOCILocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		access, target, globalAccessPolicy = &tr.Spec.Access, &tr.Spec.Repository, tr.Spec.GlobalAccessPolicy
		component, version, targetResource, output = tr.Spec.Component, tr.Spec.Version, tr.Spec.TargetResource, &tr.Output
	case *uploadv1alpha1.TransferS3ToCTFLocalBlob:
		if tr.Spec == nil {
			return nil, fmt.Errorf("spec is required for %s transformation", tr.Type)
		}
		access, target = &tr.Spec.Access, &tr.Spec.Repository
		component, version, targetResource, output = tr.Spec.Component, tr.Spec.Version, tr.Spec.TargetResource, &tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}
	switch {
	case targetResource == nil:
		return nil, fmt.Errorf("target resource is required")
	case component == "":
		return nil, fmt.Errorf("component name is required")
	case version == "":
		return nil, fmt.Errorf("component version is required")
	}
	if err := validateSourceAccess(access); err != nil {
		return nil, fmt.Errorf("invalid source access: %w", err)
	}

	// The source resource is the target resource with the source access: both describe
	// the same content, so the target digest is what the downloaded content must match.
	source := descriptor.ConvertFromV2Resource(targetResource)
	source.Access = access

	repo, err := ocitransformer.OpenLocalBlobTarget(ctx, t.RepoProvider, t.CredentialProvider, target, globalAccessPolicy)
	if err != nil {
		return nil, err
	}

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, source); err == nil && consumerID != nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving source credentials: %w", err)
			}
		}
	}
	content, err := t.ResourceRepository.DownloadResource(ctx, source, creds)
	if err != nil {
		return nil, fmt.Errorf("failed downloading resource %v: %w", source.ToIdentity(), err)
	}

	updated, err := repo.AddLocalResource(ctx, component, version, descriptor.ConvertFromV2Resource(targetResource), content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w", targetResource.Name, component, version, err)
	}
	v2Resource, err := descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updated)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	*output = &uploadv1alpha1.TransferToLocalBlobOutput{Resource: v2Resource}
	return transformation, nil
}

// validateSourceAccess checks what the schema of a source access cannot express.
func validateSourceAccess(access runtime.Typed) error {
	if validator, ok := access.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return err
		}
	}
	if wget, ok := access.(*wgetaccessv1.Wget); ok && !isRepeatableWgetVerb(wget.Verb) {
		return fmt.Errorf("verb %q cannot be streamed, only GET can", wget.Verb)
	}
	return nil
}
