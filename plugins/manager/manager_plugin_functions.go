package manager

import (
	"context"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetReadWriteComponentVersionRepositoryForType returns a constructed plugin for a given type with required capacities.
// If such a plugin does not exist, it throws an error.
// Multiple plugins upload the same resource to multiple locations -> because they register for the same resource type.
// All the plugins should be returned in that case and then being used for that resource during upload.
// Selection based on Resource, selector /matchers/ provide the correct plugin for that.
// N resource plugin -> Narrow down the plugins with Selectors. -> This is done by the user -< Needs to be able to filter then down based on the selector.
// Filter them using the matcher N:M for resource:plugin.
// Exp: For every resource with type Helm use the plugin with Identity Helm Uploader.
// Match a Resource Label/Attribute to the Plugin Identity.
// Create a Configuration for the Plugin Discovery. This Discovery should have the ability to add additional information
// for that plugin.
// This is another way but this declared and we loose the dynamic loading.
// plugins:
//   - name: bla
//     location: /bla
//     type: bla
//     identity:
//   - uploader: helm
//   - name: bla2
//     location: /bla2
//     type: bla2
//     identity:
//   - uploader: helm
//
// Scenario2:
//
//	The plugin needs to declare a sensible set of identities that can be used to configure them by.
//	ID: Is already unique -> something unique that is different in the entire plugin system ( could be something that comes from outside the filesystem )
//	Orchestrator decides -> User configuration tells the orchestrator which plugin to use.
//	On Conflict error out by default, otherwise fine-tune via config selectors
//
// F: Get Call cannot be adjusted based on what I want?
// Just return many plugins for now.
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

// GetGenericPluginForType finds generic plugin types for the given repository type.
func (pm *PluginManager) GetGenericPluginForType(ctx context.Context, typ runtime.Typed) ([]GenericPluginContract, error) {
	plugins, err := fetchPlugin[GenericPluginContract](ctx, typ, GenericRepositoryCapability, pm)
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
