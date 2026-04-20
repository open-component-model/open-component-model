package wget

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	wgetrepo "ocm.software/open-component-model/bindings/go/wget/repository"
)

func Register(resRegistry *resource.ResourceRegistry) error {
	return resRegistry.RegisterInternalResourcePlugin(
		wgetrepo.NewResourceRepository(),
	)
}
