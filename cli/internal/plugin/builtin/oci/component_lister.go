package oci

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/ctf"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CTFComponentListerPlugin is a built-in CLI plug-in that facilitates listing of OCM components stored in a CTF repository.
// The plug-in implements the InternalComponentListerPluginContract interface.
type CTFComponentListerPlugin struct{}

var _ componentlister.InternalComponentListerPluginContract = (*CTFComponentListerPlugin)(nil)

var ErrWrongUsage = errors.New("wrong usage of CTF component lister plugin")

func (l *CTFComponentListerPlugin) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	return ocirepository.Scheme
}

// GetComponentLister returns a component lister for the given CTF repository specification.
// If the provided specification is not of type *ctfv1.Repository, an error is returned.
func (l *CTFComponentListerPlugin) GetComponentLister(ctx context.Context, repositorySpecification runtime.Typed, _ map[string]string) (repository.ComponentLister, error) {
	ctfRepoSpec, ok := repositorySpecification.(*ctfv1.Repository)
	if !ok {
		return nil, errors.Join(ErrWrongUsage, fmt.Errorf("not a CTF repository type: %T", repositorySpecification))
	}

	archive, err := ctf.OpenCTFFromOSPath(ctfRepoSpec.FilePath, ctf.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error opening CTF archive: %w", err)
	}

	return ocictf.NewComponentLister(archive), nil
}

// GetComponentListerCredentialConsumerIdentity retrieves an identity for the given repository specification.
// Since CTF repositories do not require credentials, this method always returns nil and an error indicating that credentials are not supported or needed.
func (l *CTFComponentListerPlugin) GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, errors.Join(ErrWrongUsage, errors.New("credentials not supported"))
}
