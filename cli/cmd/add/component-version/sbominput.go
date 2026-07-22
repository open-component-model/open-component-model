package componentversion

import (
	"encoding/json"
	"fmt"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	sbomspec "ocm.software/open-component-model/bindings/go/input/sbom/spec/v1"
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

// resolveSBOMInputs resolves each SBoM/v1 input's subject reference (by name) to
// the referenced resource's access, embedding that access into the input spec so
// the input method has a self-contained access to run discovery against.
//
// It operates on the parsed runtime constructor before construction, where every
// sibling resource and its access are visible. The referenced resource's access
// is static input data (e.g. an OCI image reference), so no construction ordering
// is required. An SBoM/v1 input whose subject is missing or has no access is a
// hard error.
func resolveSBOMInputs(spec *constructorruntime.ComponentConstructor) error {
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

		for ri := range component.Resources {
			res := &component.Resources[ri]
			if !res.HasInput() {
				continue
			}
			if !isSBOMInputType(res.Input.GetType()) {
				continue
			}
			if err := resolveOneSBOMInput(component.Name, res, accessByName); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveOneSBOMInput injects the subject resource's access into a single
// SBoM/v1 input spec, in place.
func resolveOneSBOMInput(componentName string, res *constructorruntime.Resource, accessByName map[string]runtime.Typed) error {
	raw, ok := res.Input.(*runtime.Raw)
	if !ok {
		return fmt.Errorf("sbom input for resource %q in component %q has unexpected internal type %T", res.Name, componentName, res.Input)
	}

	spec := sbomspec.SBOM{}
	if err := json.Unmarshal(raw.Data, &spec); err != nil {
		return fmt.Errorf("parsing sbom input for resource %q in component %q failed: %w", res.Name, componentName, err)
	}

	subject := spec.Resource[constructorruntime.IdentityAttributeName]
	if subject == "" {
		return fmt.Errorf("sbom input for resource %q in component %q must reference a resource by name (resource.name)", res.Name, componentName)
	}
	if spec.Access != nil {
		// Already resolved (e.g. re-run); nothing to do.
		return nil
	}

	access, found := accessByName[subject]
	if !found {
		return fmt.Errorf("sbom input for resource %q references resource %q which does not exist or has no access in component %q", res.Name, subject, componentName)
	}

	// Serialize the subject's access into a Raw and embed it into the spec.
	var accessRaw runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(access, &accessRaw); err != nil {
		return fmt.Errorf("serializing access of resource %q for sbom input %q failed: %w", subject, res.Name, err)
	}
	spec.Access = &accessRaw

	// Re-marshal the enriched spec back into the input Raw so the downstream
	// input method sees the embedded access.
	enriched, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("re-marshalling resolved sbom input for resource %q failed: %w", res.Name, err)
	}
	raw.Data = enriched
	return nil
}
