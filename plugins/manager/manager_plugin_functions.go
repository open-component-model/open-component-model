package manager

import (
	"context"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func (pm *PluginManager) GetReadWriteComponentVersionRepositoryForType(ctx context.Context, typ runtime.Typed) ([]ReadWriteRepositoryPluginContract, error) {
	plugins, err := fetchPlugin[ReadWriteRepositoryPluginContract](
		ctx,
		typ,
		ReadWriteComponentVersionRepositoryCapability,
		pm,
	)
	if err != nil {
		return nil, err
	}

	return plugins, nil
}

func (pm *PluginManager) GetReadOCMRepoPlugin(ctx context.Context, repoType runtime.Typed) ([]ReadRepositoryPluginContract, error) {
	plugins, err := fetchPlugin[ReadRepositoryPluginContract](ctx, repoType, ReadComponentVersionRepositoryCapability, pm)
	if err != nil {
		return nil, err
	}

	return plugins, nil
}

func (pm *PluginManager) GetWriteOCMRepoPlugin(ctx context.Context, repoType runtime.Typed) ([]WriteRepositoryPluginContract, error) {
	plugins, err := fetchPlugin[WriteRepositoryPluginContract](ctx, repoType, WriteComponentVersionRepositoryCapability, pm)
	if err != nil {
		return nil, err
	}

	return plugins, nil
}

func (pm *PluginManager) GetReadResourceRepoPlugin(ctx context.Context, repoType runtime.Typed) ([]ReadResourcePluginContract, error) {
	plugins, err := fetchPlugin[ReadResourcePluginContract](ctx, repoType, ReadResourceRepositoryCapability, pm)
	if err != nil {
		return nil, err
	}

	return plugins, nil
}

func (pm *PluginManager) GetWriteResourceRepoPlugin(ctx context.Context, repoType runtime.Typed) ([]WriteResourcePluginContract, error) {
	plugins, err := fetchPlugin[WriteResourcePluginContract](ctx, repoType, WriteResourceRepositoryCapability, pm)
	if err != nil {
		return nil, err
	}

	return plugins, nil
}

//
//func (pm *PluginManager) GetCredentialRepositoryPlugins(ctx context.Context, repoType runtime.Typed) ([]CredentialRepositoryPluginContract, error) {
//	plugins, err := fetchPlugin[CredentialRepositoryPluginContract](ctx, repoType, CredentialRepositoryPluginCapability, pm)
//	if err != nil {
//		return nil, err
//	}
//
//	return plugins, nil
//}
//
//func (pm *PluginManager) GetCredentialPlugins(ctx context.Context, repoType runtime.Typed) ([]CredentialPluginContract, error) {
//	plugins, err := fetchPlugin[CredentialPluginContract](ctx, repoType, CredentialPluginCapability, pm)
//	if err != nil {
//		return nil, err
//	}
//	return plugins, nil
//}
//
//func (pm *PluginManager) GetTransformerPlugin(ctx context.Context, repoType runtime.Typed) ([]TransformerPluginContract, error) {
//	plugins, err := fetchPlugin[TransformerPluginContract](ctx, repoType, TransformerCapability, pm)
//	if err != nil {
//		return nil, err
//	}
//
//	return plugins, nil
//}
