package sbom

import (
	"context"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	sbominput "ocm.software/open-component-model/bindings/go/input/sbom"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Register wires the SBoM/v1 input method into the CLI plugin registries. The
// input method discovers on-image SBOMs at construction time by delegating to the
// resource-plugin registry (the builtin OCI plugin implements
// oci.ImageSBOMDownloader).
func Register(inputRegistry *input.RepositoryRegistry, resourcePluginRegistry *resource.ResourceRegistry) error {
	method := &sbominput.InputMethod{
		Discoverer: &registryDiscoverer{resources: resourcePluginRegistry},
	}
	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register sbom resource input method: %w", err)
	}
	return nil
}

// registryDiscoverer resolves the resource plugin for an access and, when that
// plugin supports on-image SBOM discovery, delegates to it.
type registryDiscoverer struct {
	resources *resource.ResourceRegistry
}

var _ sbominput.ImageSBOMDiscoverer = (*registryDiscoverer)(nil)

func (d *registryDiscoverer) ResolveCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	plugin, err := d.resources.GetResourcePlugin(ctx, res.GetAccess())
	if err != nil {
		return nil, fmt.Errorf("no resource plugin for access %q: %w", res.GetAccess().GetType(), err)
	}
	return plugin.GetResourceCredentialConsumerIdentity(ctx, res)
}

func (d *registryDiscoverer) DiscoverImageSBOMs(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) ([]oci.SBOM, error) {
	plugin, err := d.resources.GetResourcePlugin(ctx, res.GetAccess())
	if err != nil {
		return nil, fmt.Errorf("no resource plugin for access %q: %w", res.GetAccess().GetType(), err)
	}
	downloader, ok := plugin.(oci.ImageSBOMDownloader)
	if !ok {
		return nil, fmt.Errorf("resource plugin for access %q does not support on-image SBOM discovery", res.GetAccess().GetType())
	}
	return downloader.DownloadImageSBOMs(ctx, res, credentials)
}
