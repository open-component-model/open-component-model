package componentversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	sbomspec "ocm.software/open-component-model/bindings/go/input/sbom/spec/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// sbomInputVersionedType and sbomInputUnversionedType (plus their lowercase
// legacy aliases) identify the SBoM/v1 input spec in the parsed constructor.
var (
	sbomInputVersionedType   = runtime.NewVersionedType(sbomspec.Type, sbomspec.Version)
	sbomInputUnversionedType = runtime.NewUnversionedType(sbomspec.Type)
	sbomInputLegacyVersioned = runtime.NewVersionedType(sbomspec.LegacyType, sbomspec.Version)
	sbomInputLegacyType      = runtime.NewUnversionedType(sbomspec.LegacyType)
)

func isSBOMInputType(t runtime.Type) bool {
	return t.Equal(sbomInputVersionedType) ||
		t.Equal(sbomInputUnversionedType) ||
		t.Equal(sbomInputLegacyVersioned) ||
		t.Equal(sbomInputLegacyType)
}

// platformLister enumerates the platforms of a resource's OCI image. It is
// implemented by the CLI over the resource-plugin registry and injected so the
// pre-pass stays testable. It returns nil for single-platform images.
type platformLister interface {
	ListImagePlatforms(ctx context.Context, resource *descriptor.Resource) ([]ociImageSpecV1.Platform, error)
}

// resolveSBOMInputs resolves each SBoM/v1 input's subject reference (by name) to
// the referenced resource's access and embeds it into the input spec, so the
// input method has a self-contained access to run discovery against.
//
// For a multi-arch image with no explicit platform, the SBOM of every platform is
// attached: the single authored SBoM/v1 resource is expanded into one resource per
// platform, each pinned to that platform and tagged with a matching extraIdentity
// (os/architecture[/variant]). An explicit platform, or a single-platform image,
// yields exactly one resource (unchanged behavior).
//
// It operates on the parsed runtime constructor before construction, where every
// sibling resource and its access are visible. An SBoM/v1 input whose subject is
// missing or has no access is a hard error.
func resolveSBOMInputs(ctx context.Context, spec *constructorruntime.ComponentConstructor, lister platformLister) error {
	if spec == nil {
		return nil
	}
	for ci := range spec.Components {
		component := &spec.Components[ci]

		// Build a name -> access map from resources that carry an access.
		accessByName := make(map[string]runtime.Typed)
		for ri := range component.Resources {
			res := &component.Resources[ri]
			if res.HasAccess() && res.Name != "" {
				accessByName[res.Name] = res.Access
			}
		}

		// Rebuild the resources slice, expanding multi-arch SBOM inputs in place.
		expanded := make([]constructorruntime.Resource, 0, len(component.Resources))
		for ri := range component.Resources {
			res := &component.Resources[ri]
			if !res.HasInput() || !isSBOMInputType(res.Input.GetType()) {
				expanded = append(expanded, *res)
				continue
			}
			resolved, err := resolveOneSBOMInput(ctx, component.Name, res, accessByName, lister)
			if err != nil {
				return err
			}
			expanded = append(expanded, resolved...)
		}
		component.Resources = expanded
	}
	return nil
}

// resolveOneSBOMInput resolves a single SBoM/v1 resource, returning one resource
// (single-platform / explicit platform) or N resources (multi-arch expansion).
func resolveOneSBOMInput(ctx context.Context, componentName string, res *constructorruntime.Resource, accessByName map[string]runtime.Typed, lister platformLister) ([]constructorruntime.Resource, error) {
	raw, ok := res.Input.(*runtime.Raw)
	if !ok {
		return nil, fmt.Errorf("sbom input for resource %q in component %q has unexpected internal type %T", res.Name, componentName, res.Input)
	}

	spec := sbomspec.SBOM{}
	if err := json.Unmarshal(raw.Data, &spec); err != nil {
		return nil, fmt.Errorf("parsing sbom input for resource %q in component %q failed: %w", res.Name, componentName, err)
	}

	subject := spec.Resource.Name
	if subject == "" {
		return nil, fmt.Errorf("sbom input for resource %q in component %q must reference a resource by name (resource.name)", res.Name, componentName)
	}
	if spec.Access != nil {
		// Already resolved (e.g. re-run); leave as-is.
		return []constructorruntime.Resource{*res}, nil
	}

	access, found := accessByName[subject]
	if !found {
		return nil, fmt.Errorf("sbom input for resource %q references resource %q which does not exist or has no access in component %q", res.Name, subject, componentName)
	}

	var accessRaw runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(access, &accessRaw); err != nil {
		return nil, fmt.Errorf("serializing access of resource %q for sbom input %q failed: %w", subject, res.Name, err)
	}
	spec.Access = &accessRaw

	// Explicit architecture selector, or no lister: one resource for that arch.
	if arch := spec.Resource.Architecture(); arch != "" || lister == nil {
		var platform *ociImageSpecV1.Platform
		if arch != "" {
			// Tag the baked resource with the selected platform so it can be
			// identified per-arch on download, mirroring the expansion path.
			platform = &ociImageSpecV1.Platform{
				OS:           spec.Resource.ExtraIdentity["os"],
				Architecture: arch,
				Variant:      spec.Resource.ExtraIdentity["variant"],
			}
		}
		out, err := sbomResourceWithSpec(res, spec, platform)
		if err != nil {
			return nil, err
		}
		return []constructorruntime.Resource{out}, nil
	}

	// Enumerate the image's platforms to decide whether to expand.
	syntheticSubject := &descriptor.Resource{}
	syntheticSubject.Access = &accessRaw
	platforms, err := lister.ListImagePlatforms(ctx, syntheticSubject)
	if err != nil {
		return nil, fmt.Errorf("listing platforms of resource %q for sbom input %q failed: %w", subject, res.Name, err)
	}

	// Single-platform image: one resource, no arch selector, no extraIdentity.
	if len(platforms) <= 1 {
		out, err := sbomResourceWithSpec(res, spec, nil)
		if err != nil {
			return nil, err
		}
		return []constructorruntime.Resource{out}, nil
	}

	// Multi-arch: one resource per platform, each with a matching arch selector in
	// the reference and a matching extraIdentity on the baked resource.
	out := make([]constructorruntime.Resource, 0, len(platforms))
	for i := range platforms {
		p := platforms[i]
		perArch := spec
		perArch.Resource = withPlatformSelector(spec.Resource, p)
		r, err := sbomResourceWithSpec(res, perArch, &p)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// withPlatformSelector returns a copy of the reference with os/architecture/variant
// set in ExtraIdentity to pin one platform.
func withPlatformSelector(ref sbomspec.ResourceReference, p ociImageSpecV1.Platform) sbomspec.ResourceReference {
	out := ref
	out.ExtraIdentity = runtime.Identity{}
	for k, v := range ref.ExtraIdentity {
		out.ExtraIdentity[k] = v
	}
	if p.OS != "" {
		out.ExtraIdentity["os"] = p.OS
	}
	out.ExtraIdentity["architecture"] = p.Architecture
	if p.Variant != "" {
		out.ExtraIdentity["variant"] = p.Variant
	}
	return out
}

// sbomResourceWithSpec deep-copies the authored resource, re-marshals the given
// spec into its input, and (when platform is non-nil) sets the os/architecture
// extraIdentity that disambiguates per-arch SBOM resources.
func sbomResourceWithSpec(base *constructorruntime.Resource, spec sbomspec.SBOM, platform *ociImageSpecV1.Platform) (constructorruntime.Resource, error) {
	out := *base.DeepCopy()

	data, err := json.Marshal(spec)
	if err != nil {
		return constructorruntime.Resource{}, fmt.Errorf("re-marshalling resolved sbom input for resource %q failed: %w", base.Name, err)
	}
	out.Input = &runtime.Raw{Type: base.Input.GetType(), Data: data}

	if platform != nil {
		if out.ExtraIdentity == nil {
			out.ExtraIdentity = runtime.Identity{}
		}
		if platform.OS != "" {
			out.ExtraIdentity["os"] = platform.OS
		}
		out.ExtraIdentity["architecture"] = platform.Architecture
		if platform.Variant != "" {
			out.ExtraIdentity["variant"] = platform.Variant
		}
	}
	return out, nil
}

// platformString renders a platform as "os/arch" or "os/arch/variant".
func platformString(p ociImageSpecV1.Platform) string {
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}
	return s
}

// resourcePluginPlatformLister implements platformLister over the resource-plugin
// registry: it resolves the plugin for a resource's access, resolves credentials,
// and delegates to the plugin's ImagePlatformLister capability. A resource whose
// access has no such plugin (e.g. a non-OCI access) yields nil.
type resourcePluginPlatformLister struct {
	pluginManager   *manager.PluginManager
	credentialGraph credentials.Resolver
}

func (l *resourcePluginPlatformLister) ListImagePlatforms(ctx context.Context, res *descriptor.Resource) ([]ociImageSpecV1.Platform, error) {
	access := res.GetAccess()
	if access == nil {
		return nil, nil
	}
	plugin, err := l.pluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, access)
	if err != nil {
		return nil, nil
	}
	lister, ok := plugin.(oci.ImagePlatformLister)
	if !ok {
		return nil, nil
	}

	var creds runtime.Typed
	if credIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, res); err == nil {
		if creds, err = l.credentialGraph.Resolve(ctx, credIdentity); err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("getting credentials for platform listing failed: %w", err)
		}
	}
	return lister.ListImagePlatforms(ctx, res, creds)
}

