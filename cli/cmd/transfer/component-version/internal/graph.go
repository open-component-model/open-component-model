package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/dag"
	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

func BuildGraphDefinition(
	ctx context.Context,
	fromSpec *compref.Ref,
	toSpec runtime.Typed,
	repoResolver resolvers.ComponentVersionRepositoryResolver,
	opts ...Option,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	o := Options{}
	for _, opt := range opts {
		opt(&o)
	}

	disc := &discoverer{
		recursive:         o.Recursive,
		discoveredDigests: make(map[string]descriptor.Digest),
	}
	res := &resolver{
		repoResolver: repoResolver,
		expectedDigest: func(id runtime.Identity) *descriptor.Digest {
			disc.mu.Lock()
			defer disc.mu.Unlock()
			if !disc.recursive {
				return nil
			}
			dig, ok := disc.discoveredDigests[id.String()]
			if !ok {
				return nil
			}
			return &dig
		},
	}

	root := fromSpec.String()

	dr := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, *discoveryValue]{
		Roots:      []string{root},
		Resolver:   res,
		Discoverer: disc,
	})

	if err := dr.Discover(ctx); err != nil {
		return nil, fmt.Errorf("recursive discovery failed: %w", err)
	}

	tgd := &transformv1alpha1.TransformationGraphDefinition{
		Environment: &runtime.Unstructured{
			Data: map[string]any{
				"to": AsUnstructured(toSpec).Data,
			},
		},
	}

	g := dr.Graph()
	err := g.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		return fillGraphDefinitionWithPrefetchedComponents(d, toSpec, tgd, o.CopyMode, o.UploadType)
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

func fillGraphDefinitionWithPrefetchedComponents(d *dag.DirectedAcyclicGraph[string], toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition, copyMode CopyMode, uploadType UploadType) error {
	for _, v := range d.Vertices {
		val := v.Attributes[dagsync.AttributeValue].(*discoveryValue)
		ref := val.Ref

		id := identityToTransformationID(ref.Identity())

		v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), val.Descriptor)
		if err != nil {
			return fmt.Errorf("cannot convert to v2: %w", err)
		}

		// Track resource transformation IDs for building descriptor
		resourceTransformIDs := make(map[int]string)

		// Process local resources and OCI artifacts
		for i, resource := range v2desc.Component.Resources {
			access, err := Scheme.NewObject(resource.Access.Type)
			if err != nil {
				return fmt.Errorf("cannot create new object for resource access type %q: %w", resource.Access.Type.String(), err)
			}
			if err := Scheme.Convert(resource.Access, access); err != nil {
				return fmt.Errorf("cannot convert resource access to typed object: %w", err)
			}

			if copyMode == CopyModeLocalBlobResources && !descriptorv2.IsLocalBlob(access) {
				slog.Info("Skipping copy of resource since its access type is not a local blob. Only resources with local blob access are copied when CopyModeLocalBlobResources is set.",
					"component", ref.Component, "version", ref.Version, "resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String())
				continue
			}

			switch access.(type) {
			case *descriptorv2.LocalBlob:
				processLocalBlob(resource, id, ref, tgd, toSpec, resourceTransformIDs, i)
			case *ociv1.OCIImage:
				uploadAsOCIArtifact := uploadType != UploadAsLocalBlob
				err := processOCIArtifact(resource, id, ref, tgd, toSpec, resourceTransformIDs, i, uploadAsOCIArtifact)
				if err != nil {
					return fmt.Errorf("cannot process OCI artifact resource: %w", err)
				}
			default:
				// No transformation configured for resource with access types not listed above
				slog.Info("Unsupported resource access type, skipping resource. Only local blob and OCI artifact resources are supported for transformation.",
					"component", ref.Component, "version", ref.Version, "resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String())
			}
		}

		// Marshal original v2 descriptor to environment
		rawV2Desc, err := json.Marshal(v2desc)
		if err != nil {
			return fmt.Errorf("cannot marshal v2 descriptor: %w", err)
		}
		mapDesc := make(map[string]any)
		if err := json.Unmarshal(rawV2Desc, &mapDesc); err != nil {
			return fmt.Errorf("cannot unmarshal v2 descriptor: %w", err)
		}

		tgd.Environment.Data[id] = mapDesc

		// Build upload transformation
		// If there are local resources, we need to reconstruct the descriptor with updated resources
		var descriptorSpec any
		if len(resourceTransformIDs) > 0 {
			// Build resources array with CEL expressions for updated resources
			resourcesArray := make([]any, len(v2desc.Component.Resources))
			for i := range v2desc.Component.Resources {
				if addID, ok := resourceTransformIDs[i]; ok {
					// Reference updated resource from AddLocalResource output using CEL
					resourcesArray[i] = fmt.Sprintf("${%s.output.resource}", addID)
				} else {
					// Reference original resource from environment using CEL
					resourcesArray[i] = fmt.Sprintf("${environment.%s.component.resources[%d]}", id, i)
				}
			}

			// Build descriptor with updated resources and all required fields
			// All fields are required by the schema even if null
			componentMap := map[string]any{
				"name":      fmt.Sprintf("${environment.%s.component.name}", id),
				"version":   fmt.Sprintf("${environment.%s.component.version}", id),
				"provider":  fmt.Sprintf("${environment.%s.component.provider}", id),
				"resources": resourcesArray,
			}

			// Add optional fields - reference from environment if present, otherwise null
			if v2desc.Component.RepositoryContexts != nil {
				componentMap["repositoryContexts"] = fmt.Sprintf("${environment.%s.component.repositoryContexts}", id)
			} else {
				componentMap["repositoryContexts"] = nil
			}

			if v2desc.Component.Sources != nil {
				componentMap["sources"] = fmt.Sprintf("${environment.%s.component.sources}", id)
			} else {
				componentMap["sources"] = nil
			}

			if v2desc.Component.References != nil {
				componentMap["componentReferences"] = fmt.Sprintf("${environment.%s.component.componentReferences}", id)
			} else {
				componentMap["componentReferences"] = nil
			}

			descriptorSpec = map[string]any{
				"meta":      fmt.Sprintf("${environment.%s.meta}", id),
				"component": componentMap,
			}
		} else {
			// No local resources, use original descriptor from environment
			descriptorSpec = fmt.Sprintf("${environment.%s}", id)
		}

		upload := transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: ChooseAddType(toSpec),
				ID:   id + "Upload",
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"repository": AsUnstructured(toSpec).Data,
				"descriptor": descriptorSpec,
			}},
		}

		tgd.Transformations = append(tgd.Transformations, upload)
	}
	return nil
}
