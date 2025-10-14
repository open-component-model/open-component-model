package oci

import (
	"context"
	"errors"

	"ocm.software/open-component-model/bindings/go/ctf"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentlister"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type CTFComponentListerPlugin struct{}

var _ componentlister.InternalComponentListerPluginContract = (*CTFComponentListerPlugin)(nil)

func (l *CTFComponentListerPlugin) GetComponentLister(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentLister, error) {
	var archive ctf.CTF
	return ocictf.NewComponentLister(archive), nil
}

func (l *CTFComponentListerPlugin) GetComponentListerCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, errors.New("CTF component lister does not support/need credentials")
}
