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
	githubv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// TransferRoot pairs a DAG root key with its target repositories and source resolver.
//
// Design notes:
//   - SourceResolver is intentionally a single resolver per component: a component version
//     has exactly one source, even when it is pushed to multiple targets. The component is
//     fetched once and uploaded to all Targets. Callers that supply conflicting resolvers for
//     the same component key receive an error from collectTransferRoots before
//     BuildGraphDefinition is invoked.
//   - There is intentionally no TargetSpecResolver to mirror SourceResolver. Target
//     repositories are opened directly from their runtime.Typed specs by the builder
//     (transform executor), not by the transfer layer. Dynamic per-component target routing
//     can be added in the future if needed.
//   - During recursive discovery, child components inherit their parent's Targets and
//     SourceResolver. All components in a dependency tree are therefore transferred to the
//     same set of targets as the root that references them.
type TransferRoot struct {
	// RootComponentKey is the "component:version" string used as a DAG root.
	RootComponentKey string
	// Targets is the list of target repository specs this component should be transferred to.
	// Each spec is opened independently by the builder; the transfer layer only tracks which
	// targets exist, not how to open them. Note that there is intentionally no
	// TargetSpecResolver here — see the struct doc for the rationale.
	Targets []runtime.Typed
	// SourceResolver resolves this root's component version from its source repository.
	// One resolver per component — see the struct doc for the design rationale.
	SourceResolver resolvers.ComponentVersionRepositoryResolver
}

// BuildGraphDefinition constructs a [transformv1alpha1.TransformationGraphDefinition] for
// transferring component versions (and optionally their resources) from source to target(s).
//
// The process has two phases:
//
//  1. Discovery: A concurrent DAG discoverer resolves each root component and, if recursion is
//     enabled, follows component references to build a complete dependency graph. During discovery, each
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
	roots map[string]TransferRoot,
	cfg transferv1alpha1.Config,
	uploaders []transferv1alpha1.UploaderConfig,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	// Seed the targetMap and resolverMap from explicit roots.
	// These maps are shared with the discoverer and multiResolver:
	// - targetMap: component key → list of target repository specs
	// - resolverMap: component key → resolver to fetch that component from its source
	// During recursive discovery, the discoverer will grow both maps as children are found.
	// Using a map[string]TransferRoot guarantees key uniqueness — no duplicate roots possible.
	targetMap := make(map[string][]runtime.Typed)
	resolverMap := make(map[string]resolvers.ComponentVersionRepositoryResolver)
	dagRoots := make([]string, 0, len(roots))
	for key, root := range roots {
		dagRoots = append(dagRoots, key)
		targetMap[key] = AppendUniqueRepositories(targetMap[key], root.Targets)
		resolverMap[key] = root.SourceResolver
	}

	disc := &discoverer{
		recursive:         cfg.Recursive,
		discoveredDigests: make(map[string]descruntime.Digest),
		targetMap:         targetMap,
		resolverMap:       resolverMap,
	}
	// The multiResolver delegates to per-component resolvers from resolverMap.
	// The expectedDigest closure checks whether a recursively discovered child has
	// a pinned digest from its parent's reference, enabling integrity verification.
	res := &multiResolver{
		mu:          &disc.mu,
		resolverMap: resolverMap,
		expectedDigest: func(id runtime.Identity) *descruntime.Digest {
			disc.mu.Lock()
			defer disc.mu.Unlock()
			if disc.recursive == 0 {
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
		"roots", dagRoots, "recursive", cfg.Recursive)

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
		return fillGraphDefinitionWithPrefetchedComponents(ctx, d, targetMap, tgd, uploaders)
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
//  2. For each assigned target, resource transformations are created by the uploader that
//     selects each resource, or by the baseline (see processResources).
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
	uploaders []transferv1alpha1.UploaderConfig,
) error {
	slog.DebugContext(ctx, "building transformations for discovered components",
		"components", len(d.Vertices))

	var allFileRefs []string
	// uploaderUsed records which uploaders matched a resource, so a rule that matched
	// nothing is reported instead of silently falling back to the default handlers.
	uploaderUsed := make([]bool, len(uploaders))

	// Iterate vertices in sorted key order so the emitted transformation list is
	// deterministic across runs (ranging the map directly would randomize order).
	for _, key := range d.GetVertices() {
		v := d.Vertices[key]
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

			resourceTransformIDs, fileRefs, err := processResources(ctx, v2desc, baseID, id, val, tgd, target, uploaders, uploaderUsed)
			if err != nil {
				return err
			}
			allFileRefs = append(allFileRefs, fileRefs...)

			if err := addUploadTransformation(v2desc, id, baseID, target, tgd, resourceTransformIDs,
				uploadLabel(&val.Descriptor.Component, target, targetIdx, len(targets))); err != nil {
				return err
			}
		}
	}

	addFileCleanupTransformation(tgd, allFileRefs)
	warnUnusedUploaders(ctx, uploaders, uploaderUsed)

	return nil
}

// processResources iterates over resources in a v2 descriptor and creates the appropriate
// transformations: the first uploader whose match selects a resource handles it. A resource no uploader selects follows the baseline: a local blob
// is copied as a local blob, anything else stays by reference (no transformation).
// It returns CEL spec-field expressions for all Get transformations that buffer content to disk.
func processResources(
	ctx context.Context,
	v2desc *descriptorv2.Descriptor,
	baseID string,
	id string,
	val *discoveryValue,
	tgd *transformv1alpha1.TransformationGraphDefinition,
	toSpec runtime.Typed,
	uploaders []transferv1alpha1.UploaderConfig,
	uploaderUsed []bool,
) (map[int]string, []string, error) {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version
	resourceTransformIDs := make(map[int]string)
	var fileExpressions []string
	env := &uploaderEnv{baseID: baseID, node: tgd.Environment.Data[baseID]}

	for i, resource := range v2desc.Component.Resources {
		access, err := scheme.NewObject(resource.Access.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot create new object for resource access type %q: %w", resource.Access.Type.String(), err)
		}
		if err := scheme.Convert(resource.Access, access); err != nil {
			return nil, nil, fmt.Errorf("cannot convert resource access to typed object: %w", err)
		}

		// Declaration order is significant: the first uploader whose match selects the
		// resource handles it, so more specific rules should precede broader ones. A
		// selected uploader that cannot handle the resource fails the build.
		handled := false
		if len(uploaders) > 0 {
			aliases, err := uploaderAliases(env, i, toSpec)
			if err != nil {
				return nil, nil, err
			}
			for ui, u := range uploaders {
				if u == nil {
					continue
				}
				selected, err := matches(ctx, u.EffectiveMatch(), aliases, env)
				if err != nil {
					return nil, nil, fmt.Errorf("uploader %d (%s) for resource %v: %w", ui, u.GetType(), resource.ToIdentity(), err)
				}
				if !selected {
					continue
				}
				uploaderUsed[ui] = true
				var exprs []string
				switch cfg := u.(type) {
				case *transferv1alpha1.HTTPUploaderConfig:
					err = processHTTPUploader(resource, cfg, baseID, id, val, tgd, resourceTransformIDs, i)
				case *transferv1alpha1.OCIUploaderConfig:
					exprs, err = processOCIUploader(ctx, resource, access, cfg, aliases, env, id, val, tgd, toSpec, resourceTransformIDs, i)
				case *transferv1alpha1.LocalBlobUploaderConfig:
					exprs, err = processResource(resource, access, id, val, tgd, toSpec, resourceTransformIDs, i)
				case *transferv1alpha1.ArtifactoryUploaderConfig:
					err = processRepositoryUploader(resource, access, uploadv1alpha1.ArtifactoryUploadV1alpha1, cfg.URL, cfg.Repository, cfg.Path, baseID, id, val, tgd, resourceTransformIDs, i)
				case *transferv1alpha1.NexusUploaderConfig:
					err = processRepositoryUploader(resource, access, uploadv1alpha1.NexusUploadV1alpha1, cfg.URL, cfg.Repository, cfg.Path, baseID, id, val, tgd, resourceTransformIDs, i)
				case *transferv1alpha1.ReferenceUploaderConfig:
					// No transformation: buildDescriptorSpec keeps the environment resource.
					if descriptorv2.IsLocalBlob(access) {
						err = fmt.Errorf("local blobs cannot be kept by reference (adjust match)")
					}
				default:
					return nil, nil, fmt.Errorf("unsupported uploader config type %T for resource %v", u, resource.ToIdentity())
				}
				if err != nil {
					return nil, nil, fmt.Errorf("cannot process uploader for resource %v: %w", resource.ToIdentity(), err)
				}
				fileExpressions = append(fileExpressions, exprs...)
				handled = true
				break
			}
		}
		if handled {
			continue
		}
		if !descriptorv2.IsLocalBlob(access) {
			slog.DebugContext(ctx, "no uploader selects resource, keeping it by reference",
				"component", component, "version", version,
				"resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String())
			continue
		}

		exprs, err := processResource(resource, access, id, val, tgd, toSpec, resourceTransformIDs, i)
		if err != nil {
			return nil, nil, err
		}
		fileExpressions = append(fileExpressions, exprs...)
	}
	return resourceTransformIDs, fileExpressions, nil
}

// processResource copies a single resource into the target as a local blob, dispatching on
// its access type. Each handler creates a Get transformation (fetching the resource from the
// source) and an Add transformation embedding it as a local blob in the target. It serves
// the baseline (local blobs) and a selected local blob uploader.
// It returns CEL spec-field expressions for the file buffers produced, referencing consumer spec
// fields (not producer outputs) so the DAG edge points from consumer to the cleanup node.
func processResource(resource descriptorv2.Resource, access runtime.Typed, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) ([]string, error) {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	switch acc := access.(type) {
	case *descriptorv2.LocalBlob:
		if err := processLocalBlob(resource, id, val, tgd, toSpec, resourceTransformIDs, i, ""); err != nil {
			return nil, fmt.Errorf("failed processing local blob resource: %w", err)
		}
		return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
	case *ociv1.OCIImage:
		if err := processOCIArtifact(resource, id, val, tgd, toSpec, resourceTransformIDs, i); err != nil {
			return nil, fmt.Errorf("cannot process OCI artifact resource: %w", err)
		}
		return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
	case *ociv1.OCIImageLayer:
		// A layer is a single blob, not an OCI artifact: it is always copied by value.
		if err := processOCIImageLayer(resource, id, val, tgd, toSpec, resourceTransformIDs, i); err != nil {
			return nil, fmt.Errorf("cannot process OCI image layer resource: %w", err)
		}
		return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
	case *helmv1.Helm:
		if err := processHelm(resource, id, val, tgd, toSpec, resourceTransformIDs, i, ""); err != nil {
			return nil, fmt.Errorf("cannot process Helm Chart resource: %w", err)
		}
		return helmFileExpressions(id, resourceID), nil
	case *wgetv1.Wget:
		// A wget resource is a plain blob: download it and embed it as a local blob in the
		// target. There is no OCI-artifact representation.
		if err := processWget(resource, id, val, tgd, toSpec, resourceTransformIDs, i); err != nil {
			return nil, fmt.Errorf("cannot process wget resource: %w", err)
		}
		return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
	case *s3v2.S3:
		// An S3 resource is a plain blob: download it and embed it as a local blob in the
		// target. There is no OCI-artifact representation.
		if err := processS3(resource, id, val, tgd, toSpec, resourceTransformIDs, i); err != nil {
			return nil, fmt.Errorf("cannot process s3 resource: %w", err)
		}
		return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
	case *githubv1.GitHub:
		if err := processGitHub(resource, acc, id, val, tgd, toSpec, resourceTransformIDs, i); err != nil {
			return nil, fmt.Errorf("cannot process GitHub resource: %w", err)
		}
		return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
	default:
		return nil, fmt.Errorf("local blob uploader cannot copy access type %s (adjust match)", resource.Access.Type)
	}
}

// warnUnusedUploaders logs every uploader whose match selected no resource of the transfer.
// A common cause is a match written against the access a resource gets in the target (such
// as a local blob after copying) instead of its access in the source component version.
func warnUnusedUploaders(ctx context.Context, uploaders []transferv1alpha1.UploaderConfig, used []bool) {
	for idx, u := range uploaders {
		if u == nil || used[idx] {
			continue
		}
		slog.WarnContext(ctx, "uploader selected no resource; its match is evaluated against the resource as described in the source component version",
			"uploader", u.GetType().String(),
			"index", idx,
			"match", u.EffectiveMatch())
	}
}

// helmFileExpressions returns the CEL spec-field expressions of the file buffers a Helm
// chart transfer produces (see processHelm), for cleanup.
func helmFileExpressions(id, resourceID string) []string {
	convertResourceID := fmt.Sprintf("%sConvert%s", id, resourceID)
	return []string{
		fmt.Sprintf("${%s.spec.chartFile}", convertResourceID),
		// provFile is optional; cleanup transformer skips empty URIs.
		fmt.Sprintf("${%s.spec.?provFile}", convertResourceID),
		fmt.Sprintf("${%sAdd%s.spec.file}", id, resourceID),
	}
}

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
func addUploadTransformation(v2desc *descriptorv2.Descriptor, id string, envID string, toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, label string) error {
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
			Type:  addType,
			ID:    id + "Upload",
			Label: label,
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

	setOptionalField(componentMap, "creationTime", id, v2desc.Component.CreationTime != "")
	setOptionalField(componentMap, "labels", id, len(v2desc.Component.Labels) != 0)
	setOptionalField(componentMap, "repositoryContexts", id, len(v2desc.Component.RepositoryContexts) != 0)
	setOptionalField(componentMap, "sources", id, len(v2desc.Component.Sources) != 0)
	setOptionalField(componentMap, "componentReferences", id, len(v2desc.Component.References) != 0)

	descSpecMap := map[string]any{
		"meta":      fmt.Sprintf("${environment.%s.meta}", id),
		"component": componentMap,
	}

	if len(v2desc.Signatures) != 0 {
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
