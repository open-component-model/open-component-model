package oci

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/ctf"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type CTFComponentListerPlugin struct{}

var _ componentlister.InternalComponentListerPluginContract = (*CTFComponentListerPlugin)(nil)

func (l *CTFComponentListerPlugin) GetComponentLister(ctx context.Context, repositorySpecification runtime.Typed, _ map[string]string) (repository.ComponentLister, error) {
	ctfRepoSpec, ok := repositorySpecification.(*ctfv1.Repository)
	if !ok {
		return nil, fmt.Errorf("not a CTF repository specification: %T", repositorySpecification)
	}

	archive, err := ctf.OpenCTFFromOSPath(ctfRepoSpec.Path, ctf.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error opening CTF archive: %w", err)
	}

	return ocictf.NewComponentLister(archive), nil
}

func (l *CTFComponentListerPlugin) GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, errors.New("CTF component lister does not support/need credentials")
}
