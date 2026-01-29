package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

func BuildGraphDefinition(
	ctx context.Context,
	fromSpec *compref.Ref,
	toSpec runtime.Typed,
	repoResolver resolvers.ComponentVersionRepositoryResolver,
	recursive bool,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	discoverer := &discoverer{
		recursive:         recursive,
		discoveredDigests: make(map[string]descriptor.Digest),
	}
	resolver := &resolver{
		repoResolver: repoResolver,
		expectedDigest: func(id runtime.Identity) *descriptor.Digest {
			discoverer.mu.Lock()
			defer discoverer.mu.Unlock()
			if !discoverer.recursive {
				return nil
			}
			dig, ok := discoverer.discoveredDigests[id.String()]
			if !ok {
				return nil
			}
			return &dig
		},
	}

	root := fromSpec.String()

	dr := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, *discoveryValue]{
		Roots:      []string{root},
		Resolver:   resolver,
		Discoverer: discoverer,
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
		return fillGraphDefinitionWithPrefetchedComponents(d, toSpec, tgd)
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

func fillGraphDefinitionWithPrefetchedComponents(d *dag.DirectedAcyclicGraph[string], toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition) error {
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
			if resource.Relation == descriptorv2.LocalRelation {
				processLocalRelation(resource, id, ref, tgd, toSpec, resourceTransformIDs, i)
			} else if isOCIArtifactAccess(resource.Access) {
				processOCIArtifact(resource, id, ref, tgd, toSpec, resourceTransformIDs, i)
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

func processOCIArtifact(resource descriptorv2.Resource, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) {
	// Process OCI artifacts (ociArtifact, ociImage, etc.)
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	// Convert resourceIdentity to map[string]any for deep copy compatibility
	resourceIdentityMap := make(map[string]any)
	for k, v := range resourceIdentity {
		resourceIdentityMap[k] = v
	}

	// Create GetOCIArtifact transformation
	getArtifactTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ChooseGetOCIArtifactType(ref.Repository),
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository":       AsUnstructured(ref.Repository).Data,
			"component":        ref.Component,
			"version":          ref.Version,
			"resourceIdentity": resourceIdentityMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getArtifactTransform)

	// Generate target reference for OCI repositories
	// Format: {baseUrl}/{subPath}/{resourceName}:{resourceVersion}
	targetReference := generateTargetImageReference(toSpec, resource.Name, resource.Version)

	// Create AddOCIArtifact transformation
	addArtifactSpec := map[string]any{
		"repository": AsUnstructured(toSpec).Data,
		"component":  ref.Component,
		"version":    ref.Version,
		"resource":   fmt.Sprintf("${%s.output.resource}", getResourceID),
		"file":       fmt.Sprintf("${%s.output.file}", getResourceID),
	}
	// Only add targetReference for OCI repositories (not CTF)
	if targetReference != "" {
		addArtifactSpec["targetReference"] = targetReference
	}

	addArtifactTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ChooseAddOCIArtifactType(toSpec),
			ID:   addResourceID,
		},
		Spec: &runtime.Unstructured{Data: addArtifactSpec},
	}
	tgd.Transformations = append(tgd.Transformations, addArtifactTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID
}

func processLocalRelation(resource descriptorv2.Resource, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) {
	// Generate transformation IDs
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	// Convert resourceIdentity to map[string]any for deep copy compatibility
	resourceIdentityMap := make(map[string]any)
	for k, v := range resourceIdentity {
		resourceIdentityMap[k] = v
	}

	// Create GetLocalResource transformation
	getResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ChooseGetLocalResourceType(ref.Repository),
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository":       AsUnstructured(ref.Repository).Data,
			"component":        ref.Component,
			"version":          ref.Version,
			"resourceIdentity": resourceIdentityMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getResourceTransform)

	// Create AddLocalResource transformation
	addResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ChooseAddLocalResourceType(toSpec),
			ID:   addResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": AsUnstructured(toSpec).Data,
			"component":  ref.Component,
			"version":    ref.Version,
			"resource":   fmt.Sprintf("${%s.output.resource}", getResourceID),
			"file":       fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID
}

type discoveryValue struct {
	Ref        *compref.Ref
	Descriptor *descriptor.Descriptor
	Digest     *descriptorv2.Digest
}

type resolver struct {
	repoResolver   resolvers.ComponentVersionRepositoryResolver
	expectedDigest func(id runtime.Identity) *descriptor.Digest
}

func (r *resolver) Resolve(ctx context.Context, key string) (*discoveryValue, error) {
	ref, err := compref.Parse(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference %q: %w", key, err)
	}

	repo, err := r.repoResolver.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx, ref.Component, ref.Version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %w", ref.Component, ref.Version, err)
	}

	if expected := r.expectedDigest(desc.Component.ToIdentity()); expected != nil {
		if err := signing.VerifyDigestMatchesDescriptor(
			ctx, desc, descriptor.Signature{Digest: *expected}, slog.Default(),
		); err != nil {
			return nil, fmt.Errorf("failed verifying resolved descriptor matches expected digest: %w", err)
		}
	}

	return &discoveryValue{
		Ref:        ref,
		Descriptor: desc,
	}, nil
}

type discoverer struct {
	mu        sync.Mutex
	recursive bool

	discoveredDigests map[string]descriptor.Digest
}

func (d *discoverer) Discover(ctx context.Context, parent *discoveryValue) ([]string, error) {
	if !d.recursive {
		return nil, nil
	}
	var children []string
	for _, ref := range parent.Descriptor.Component.References {
		childRef := &compref.Ref{
			Type:       parent.Ref.Type,
			Repository: parent.Ref.Repository,
			Prefix:     parent.Ref.Prefix,
			Component:  ref.Component,
			Version:    ref.Version,
		}
		base := childRef.String()

		if ref.Digest.Value != "" {
			d.mu.Lock()
			// TODO Panic on differing digests
			d.discoveredDigests[ref.ToComponentIdentity().String()] = ref.Digest
			d.mu.Unlock()
		}
		children = append(children, base)
	}
	return children, nil
}

var toWordRunes = []rune{',', '.', '/', '-'}

// identityToTransformationID converts a component identity to a transformation id.
func identityToTransformationID(id runtime.Identity) string {
	// TODO(jakobmoellerdev): decide if we really wanna keep such strict limits on transformation ids,
	//   if we really dont need them to be that strict.
	//   Currently Im forced to convert a map to a camel case string here.
	words := []string{"transform"}
	for _, v := range id {
		words = append(words, strings.FieldsFunc(v, func(r rune) bool {
			return slices.Contains(toWordRunes, r)
		})...)
	}
	if len(words) == 0 {
		return ""
	}
	result := strings.ToLower(words[0])
	for i := 1; i < len(words); i++ {
		w := strings.ToLower(words[i])
		if len(w) > 0 {
			result += strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return result
}
