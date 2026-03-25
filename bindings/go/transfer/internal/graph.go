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
	helmv1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// TransferRoot pairs a DAG root key with its target repositories and resolver.
type TransferRoot struct {
	// Key is the "component:version" string used as a DAG root.
	Key string
	// Targets is the list of target repository specs this component should be transferred to.
	Targets []runtime.Typed
	// Resolver resolves this root's component version from its source repository.
	Resolver resolvers.ComponentVersionRepositoryResolver
}

// BuildGraphDefinition constructs a [transformv1alpha1.TransformationGraphDefinition] for
// transferring component versions (and optionally their resources) from source to target(s).
//
// The process has two phases:
//
//  1. Discovery: A concurrent DAG discoverer resolves each root component and, if recursive is true,
//     follows component references to build a complete dependency graph. During discovery, each
//     component's target repositories and resolver are tracked in shared maps (targetMap, resolverMap)
//     that the discoverer propagates from parent to child.
//
//  2. Graph construction: For each discovered component, transformation nodes are generated for
//     every assigned target repository. Each (component, target) pair produces:
//     - Get transformations for resources (fetching from source)
//     - Add transformations for resources (uploading to target)
//     - An AddComponentVersion upload transformation for the descriptor itself
//
// The returned graph definition can be validated and executed by a builder.Builder.
func BuildGraphDefinition(
	ctx context.Context,
	roots []TransferRoot,
	recursive bool,
	copyMode int,
	uploadType int,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	// Seed the targetMap and resolverMap from explicit roots.
	// These maps are shared with the discoverer and multiResolver:
	// - targetMap: component key → list of target repository specs
	// - resolverMap: component key → resolver to fetch that component from its source
	// During recursive discovery, the discoverer will grow both maps as children are found.
	targetMap := make(map[string][]runtime.Typed)
	resolverMap := make(map[string]resolvers.ComponentVersionRepositoryResolver)
	dagRoots := make([]string, len(roots))
	for i, root := range roots {
		dagRoots[i] = root.Key
		targetMap[root.Key] = AppendUniqueRepositories(targetMap[root.Key], root.Targets)
		if _, exists := resolverMap[root.Key]; !exists {
			resolverMap[root.Key] = root.Resolver
		}
	}

	disc := &discoverer{
		recursive:         recursive,
		discoveredDigests: make(map[string]descruntime.Digest),
		targetMap:         targetMap,
		resolverMap:       resolverMap,
	}
	// The multiResolver delegates to per-component resolvers from resolverMap.
	// The expectedDigest closure checks whether a recursively discovered child has
	// a pinned digest from its parent's reference, enabling integrity verification.
	res := &multiResolver{
		resolverMap: resolverMap,
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

	slog.DebugContext(ctx, "starting component discovery",
		"roots", dagRoots, "recursive", recursive)

	dr := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, *discoveryValue]{
		Roots:      dagRoots,
		Resolver:   res,
		Discoverer: disc,
	})

	if err := dr.Discover(ctx); err != nil {
		return nil, fmt.Errorf("recursive discovery failed: %w", err)
	}

	slog.DebugContext(ctx, "component discovery completed")

	tgd := &transformv1alpha1.TransformationGraphDefinition{
		Environment: &runtime.Unstructured{
			Data: map[string]any{},
		},
	}

	// Phase 2: walk the discovered DAG and generate transformation nodes per (component, target) pair.
	g := dr.Graph()
	err := g.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		return fillGraphDefinitionWithPrefetchedComponents(ctx, d, targetMap, tgd, copyMode, uploadType)
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

// fillGraphDefinitionWithPrefetchedComponents iterates over all discovered components in the DAG
// and generates transformation nodes for each (component, target) pair.
//
// For each component:
//  1. The descriptor is converted to v2 format and added to the graph environment.
//  2. For each assigned target, resource transformations (get/add pairs) are created based
//     on the resource access type (local blob, OCI artifact, Helm chart) and the copy mode.
//  3. A final AddComponentVersion upload transformation is appended, referencing the processed
//     resources via CEL expressions.
//
// When a component has multiple targets, transformation IDs are suffixed (e.g., "T0", "T1")
// to ensure uniqueness in the DAG. The environment descriptor is shared across targets
// since it's source-side data.
func fillGraphDefinitionWithPrefetchedComponents(
	ctx context.Context,
	d *dag.DirectedAcyclicGraph[string],
	targetMap map[string][]runtime.Typed,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	copyMode int,
	uploadType int,
) error {
	slog.DebugContext(ctx, "building transformations for discovered components",
		"components", len(d.Vertices))

	for key, v := range d.Vertices {
		val := v.Attributes[dagsync.AttributeValue].(*discoveryValue)
		component := val.Descriptor.Component.Name
		version := val.Descriptor.Component.Version

		baseID := identityToTransformationID(runtime.Identity{
			descruntime.IdentityAttributeName:    component,
			descruntime.IdentityAttributeVersion: version,
		})

		v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), val.Descriptor)
		if err != nil {
			return fmt.Errorf("cannot convert to v2: %w", err)
		}

		if err := addDescriptorToEnvironment(v2desc, baseID, tgd); err != nil {
			return err
		}

		targets := targetMap[key]
		slog.DebugContext(ctx, "processing component for transfer",
			"component", component, "version", version,
			"targets", len(targets),
			"resources", len(v2desc.Component.Resources))

		for targetIdx, target := range targets {
			id := baseID
			if len(targets) > 1 {
				id = fmt.Sprintf("%sT%d", baseID, targetIdx)
			}

			slog.DebugContext(ctx, "generating transformations for target",
				"component", component, "version", version,
				"targetIndex", targetIdx, "targetType", fmt.Sprintf("%T", target),
				"transformID", id)

			resourceTransformIDs, err := processResources(ctx, v2desc, id, val, tgd, target, copyMode, uploadType)
			if err != nil {
				return err
			}

			if err := addUploadTransformation(v2desc, id, baseID, target, tgd, resourceTransformIDs); err != nil {
				return err
			}
		}
	}
	return nil
}

// processResources iterates over resources in a v2 descriptor and creates the appropriate
// get/add transformation pairs based on access type, copy mode, and upload type.
func processResources(ctx context.Context, v2desc *descriptorv2.Descriptor, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, copyMode int, uploadType int) (map[int]string, error) {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version
	resourceTransformIDs := make(map[int]string)

	for i, resource := range v2desc.Component.Resources {
		access, err := scheme.NewObject(resource.Access.Type)
		if err != nil {
			return nil, fmt.Errorf("cannot create new object for resource access type %q: %w", resource.Access.Type.String(), err)
		}
		if err := scheme.Convert(resource.Access, access); err != nil {
			return nil, fmt.Errorf("cannot convert resource access to typed object: %w", err)
		}

		if copyMode == CopyModeLocalBlobResources && !descriptorv2.IsLocalBlob(access) {
			logSkippedResource(ctx, component, version, resource, copyMode, uploadType)
			continue
		}

		if err := processResource(resource, access, id, val, tgd, toSpec, resourceTransformIDs, i, uploadType); err != nil {
			return nil, err
		}
	}
	return resourceTransformIDs, nil
}

func logSkippedResource(ctx context.Context, component, version string, resource descriptorv2.Resource, copyMode, uploadType int) {
	logLevel := slog.LevelInfo
	if uploadType == UploadAsOciArtifact {
		logLevel = slog.LevelWarn
	}
	slog.Log(ctx, logLevel,
		"Skipping copy of resource since its access type is not a local blob. Only resources with local blob access are copied when CopyModeLocalBlobResources is set.",
		"component", component,
		"version", version,
		"resource", resource.ToIdentity().String(),
		"accessType", resource.Access.Type.String(),
		"copyMode", copyMode)
}

// processResource dispatches a single resource to the appropriate handler based on its access type.
// Each handler creates a Get transformation (fetching the resource from the source) and an Add
// transformation (uploading it to the target). The uploadType and target type determine whether
// resources are stored as local blobs or separate OCI artifacts in the target repository.
func processResource(resource descriptorv2.Resource, access runtime.Typed, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadType int) error {
	_, isOCITarget := toSpec.(*oci.Repository)
	uploadAsArtifact := isOCITarget && uploadType == UploadAsOciArtifact

	switch acc := access.(type) {
	case *descriptorv2.LocalBlob:
		shouldUpload := uploadAsArtifact && isOCICompliantManifest(acc.MediaType) && acc.ReferenceName != ""
		if err := processLocalBlob(resource, acc, id, val, tgd, toSpec, resourceTransformIDs, i, shouldUpload); err != nil {
			return fmt.Errorf("failed processing local blob resource: %w", err)
		}
	case *ociv1.OCIImage:
		if err := processOCIArtifact(resource, id, val, tgd, toSpec, resourceTransformIDs, i, uploadAsArtifact); err != nil {
			return fmt.Errorf("cannot process OCI artifact resource: %w", err)
		}
	case *helmv1.Helm:
		if err := processHelm(resource, id, val, tgd, toSpec, resourceTransformIDs, i, uploadAsArtifact); err != nil {
			return fmt.Errorf("cannot process Helm Chart resource: %w", err)
		}
	default:
		slog.Info("Unsupported resource access type, skipping resource. Only local blob and OCI artifact resources are supported for transformation.",
			"component", val.Descriptor.Component.Name, "version", val.Descriptor.Component.Version,
			"resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String())
	}
	return nil
}

// addDescriptorToEnvironment marshals the v2 descriptor and adds it to the graph environment.
func addDescriptorToEnvironment(v2desc *descriptorv2.Descriptor, id string, tgd *transformv1alpha1.TransformationGraphDefinition) error {
	rawV2Desc, err := json.Marshal(v2desc)
	if err != nil {
		return fmt.Errorf("cannot marshal v2 descriptor: %w", err)
	}
	mapDesc := make(map[string]any)
	if err := json.Unmarshal(rawV2Desc, &mapDesc); err != nil {
		return fmt.Errorf("cannot unmarshal v2 descriptor: %w", err)
	}
	tgd.Environment.Data[id] = mapDesc
	return nil
}

// addUploadTransformation creates the final upload (AddComponentVersion) transformation
// for a component, reconstructing the descriptor with CEL references to modified resources.
// envID is the base ID used to reference the descriptor in the environment (without target suffix).
func addUploadTransformation(v2desc *descriptorv2.Descriptor, id string, envID string, toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string) error {
	descriptorSpec := buildDescriptorSpec(v2desc, envID, resourceTransformIDs)

	addType, err := chooseAddType(toSpec)
	if err != nil {
		return fmt.Errorf("choosing add type for target repository: %w", err)
	}

	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}

	upload := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: addType,
			ID:   id + "Upload",
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"descriptor": descriptorSpec,
		}},
	}

	tgd.Transformations = append(tgd.Transformations, upload)
	return nil
}

// buildDescriptorSpec constructs the descriptor specification for the upload transformation.
// If no resources were modified (no resource transformations), it returns a CEL reference to
// the original descriptor in the environment. Otherwise, it builds a composite descriptor where
// each modified resource is referenced via its Add transformation's output, and unmodified
// resources reference the original environment data.
func buildDescriptorSpec(v2desc *descriptorv2.Descriptor, id string, resourceTransformIDs map[int]string) any {
	if len(resourceTransformIDs) == 0 {
		return fmt.Sprintf("${environment.%s}", id)
	}

	resourcesArray := make([]any, len(v2desc.Component.Resources))
	for i := range v2desc.Component.Resources {
		if addID, ok := resourceTransformIDs[i]; ok {
			resourcesArray[i] = fmt.Sprintf("${%s.output.resource}", addID)
		} else {
			resourcesArray[i] = fmt.Sprintf("${environment.%s.component.resources[%d]}", id, i)
		}
	}

	componentMap := map[string]any{
		"name":      fmt.Sprintf("${environment.%s.component.name}", id),
		"version":   fmt.Sprintf("${environment.%s.component.version}", id),
		"provider":  fmt.Sprintf("${environment.%s.component.provider}", id),
		"resources": resourcesArray,
	}

	setOptionalField(componentMap, "repositoryContexts", id, v2desc.Component.RepositoryContexts != nil)
	setOptionalField(componentMap, "sources", id, v2desc.Component.Sources != nil)
	setOptionalField(componentMap, "componentReferences", id, v2desc.Component.References != nil)

	descSpecMap := map[string]any{
		"meta":      fmt.Sprintf("${environment.%s.meta}", id),
		"component": componentMap,
	}

	if v2desc.Signatures != nil {
		descSpecMap["signatures"] = fmt.Sprintf("${environment.%s.signatures}", id)
	}

	return descSpecMap
}

// setOptionalField sets a field in the component map, either as a CEL reference to the
// environment value if present, or nil if absent.
func setOptionalField(componentMap map[string]any, field, id string, present bool) {
	if present {
		componentMap[field] = fmt.Sprintf("${environment.%s.component.%s}", id, field)
	} else {
		componentMap[field] = nil
	}
}
