package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/dag"
	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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
		discoveredDigests: make(map[string]descruntime.Digest),
	}
	res := &resolver{
		repoResolver: repoResolver,
		expectedDigest: func(id runtime.Identity) *descruntime.Digest {
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

	root := fromSpec.Component + ":" + fromSpec.Version

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
		return fillGraphDefinitionWithPrefetchedComponents(ctx, d, toSpec, tgd, o.CopyMode, o.UploadType)
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

func fillGraphDefinitionWithPrefetchedComponents(ctx context.Context, d *dag.DirectedAcyclicGraph[string], toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition, copyMode CopyMode, uploadType UploadType) error {
	for _, v := range d.Vertices {
		val := v.Attributes[dagsync.AttributeValue].(*discoveryValue)
		component := val.Descriptor.Component.Name
		version := val.Descriptor.Component.Version

		id := identityToTransformationID(runtime.Identity{
			descruntime.IdentityAttributeName:    component,
			descruntime.IdentityAttributeVersion: version,
		})

		v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), val.Descriptor)
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
				logLevel := slog.LevelInfo
				if uploadType == UploadAsOciArtifact {
					// check if the user wants to upload as ociArtifact
					// if sure, make sure they see that copy-resources is missing
					logLevel = slog.LevelWarn
				}
				slog.Log(ctx, logLevel,
					"Skipping copy of resource since its access type is not a local blob. Only resources with local blob access are copied when CopyModeLocalBlobResources is set.",
					"component", component,
					"version", version,
					"resource", resource.ToIdentity().String(),
					"accessType", resource.Access.Type.String(),
					"copyMode", copyMode)
				continue
			}

			uploadAsOCIArtifact := false
			if uploader, ok := lookupOCIUploadSupported(access); ok {
				uploadAsOCIArtifact, err = uploader.ShouldUploadAsOCIArtifact(ctx, resource, toSpec, access, uploadType)
				if err != nil {
					return fmt.Errorf("failed to determine whether resource should be uploaded as OCI artifact: %w", err)
				}
			}
			if proc, ok := lookupProcessor(access); !ok {
				slog.Warn("Unsupported resource access type...", "component", val.Descriptor.Component.Name, "version", val.Descriptor.Component.Version, "resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String())
				continue
			} else {
				if err := proc.Process(ctx, resource, id, val, tgd, toSpec, resourceTransformIDs, i, uploadAsOCIArtifact); err != nil {
					return fmt.Errorf("failed processing resource with access type %q: %w", resource.Access.Type.String(), err)
				}
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

			descSpecMap := map[string]any{
				"meta":      fmt.Sprintf("${environment.%s.meta}", id),
				"component": componentMap,
			}

			if v2desc.Signatures != nil {
				descSpecMap["signatures"] = fmt.Sprintf("${environment.%s.signatures}", id)
			}

			descriptorSpec = descSpecMap
		} else {
			// No local resources, use original descriptor from environment
			descriptorSpec = fmt.Sprintf("${environment.%s}", id)
		}

		addType, err := ChooseAddType(toSpec)
		if err != nil {
			return fmt.Errorf("choosing add type for target repository: %w", err)
		}

		upload := transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: addType,
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
