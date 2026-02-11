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
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocirepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

var Scheme = runtime.NewScheme(runtime.WithAllowUnknown())

func init() {
	Scheme.MustRegisterScheme(oci.Scheme)
	Scheme.MustRegisterScheme(descriptorv2.Scheme)
}

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

	discoverer := &discoverer{
		recursive:           o.Recursive,
		uploadAsOCIArtifact: o.UploadAsOCIArtifact,
		discoveredDigests:   make(map[string]descriptor.Digest),
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
		uploadAsOCIArtifact: o.UploadAsOCIArtifact,
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
		return fillGraphDefinitionWithPrefetchedComponents(d, toSpec, tgd, o.CopyMode, o.UploadAsOCIArtifact)
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

func fillGraphDefinitionWithPrefetchedComponents(d *dag.DirectedAcyclicGraph[string], toSpec runtime.Typed, tgd *transformv1alpha1.TransformationGraphDefinition, copyMode CopyMode, uploadAsOCIArtifact bool) error {
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

			if copyMode == CopyModeLocalBlobResources && !shared.IsLocal(access) {
				slog.Info("Skipping copy of resource as copy mode is local blob resources only",
					"component", ref.Component, "version", ref.Version, "resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String())
				continue
			}

			switch access.(type) {
			case *descriptorv2.LocalBlob:
				processLocalBlob(resource, id, ref, tgd, toSpec, resourceTransformIDs, i)
			case *ociv1.OCIImage:
				err := processOCIArtifact(resource, id, ref, tgd, toSpec, resourceTransformIDs, i, uploadAsOCIArtifact)
				if err != nil {
					return fmt.Errorf("cannot process OCI artifact resource: %w", err)
				}
			default:
				// No transformation configured for resource with access types not listed above
				slog.Info("No copy of resource even though copy mode is copy all resources, because access type is not supported for copying",
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

func processOCIArtifact(resource descriptorv2.Resource, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	var ociAccess ociv1.OCIImage
	if err := json.Unmarshal(resource.Access.Data, &ociAccess); err != nil {
		return fmt.Errorf("cannot unmarshal OCI access: %w", err)
	}

	// e.g. ghcr.io/open-component-model/helmexample/charts/mariadb:12.2.7
	// strip the domain part and keep the rest
	referenceName, err := GetReferenceName(ociAccess)
	if err != nil {
		return fmt.Errorf("cannot get reference name: %w", err)
	}

	jRes, err := json.Marshal(resource)
	if err != nil {
		return fmt.Errorf("cannot marshal resource: %w", err)
	}
	var resourceMap map[string]any
	if err := json.Unmarshal(jRes, &resourceMap); err != nil {
		return fmt.Errorf("cannot unmarshal resource to map: %w", err)
	}

	// Create GetOCIArtifact transformation
	getArtifactTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.GetOCIArtifactV1alpha1,
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": resourceMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getArtifactTransform)

	// Create AddLocalResource transformation
	var addResourceTransform transformv1alpha1.GenericTransformation
	if uploadAsOCIArtifact {
		// Construct target Image Reference from toSpec and referenceName
		var targetImageRef string
		// Default to referenceName if we can't determine the target repo
		targetImageRef = referenceName

		if toSpec != nil {
			raw, err := json.Marshal(toSpec)
			if err == nil {
				var repoSpec ocirepo.Repository
				if err := json.Unmarshal(raw, &repoSpec); err == nil && repoSpec.BaseUrl != "" {
					targetRepoURL := repoSpec.BaseUrl
					if repoSpec.SubPath != "" {
						targetRepoURL = targetRepoURL + "/" + repoSpec.SubPath
					}
					targetImageRef = fmt.Sprintf("%s/%s", strings.TrimRight(targetRepoURL, "/"), strings.TrimLeft(referenceName, "/"))
				}
			}
		}

		addResourceTransform = transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.AddOCIArtifactVersion),
				ID:   addResourceID,
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				// "repository": AsUnstructured(toSpec).Data, // AddOCIArtifact uses repository from environment/context? No, it uses ResourceRepository which is global in transformer
				"resource": map[string]any{
					"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
					"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
					"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
					"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
					"access": map[string]interface{}{
						"type":           runtime.NewVersionedType(ociv1.LegacyType, ociv1.LegacyTypeVersion).String(),
						"imageReference": targetImageRef,
					},
				},
				"file": fmt.Sprintf("${%s.output.file}", getResourceID),
			}},
		}
	} else {
		addResourceTransform = transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: ChooseAddLocalResourceType(toSpec),
				ID:   addResourceID,
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"repository": AsUnstructured(toSpec).Data,
				"component":  ref.Component,
				"version":    ref.Version,
				"resource": map[string]any{
					// TODO(matthiasbruns): figure out how to not hate yourself doing this
					"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
					"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
					"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
					"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
					"access": map[string]interface{}{
						"type":          descriptor.GetLocalBlobAccessType().String(),
						"referenceName": referenceName,
					},
					"digest": fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
				},
				"file": fmt.Sprintf("${%s.output.file}", getResourceID),
			}},
		}
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	return nil
}

func processLocalBlob(resource descriptorv2.Resource, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) {
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
	repoResolver        resolvers.ComponentVersionRepositoryResolver
	expectedDigest      func(id runtime.Identity) *descriptor.Digest
	uploadAsOCIArtifact bool
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
	mu                  sync.Mutex
	recursive           bool
	uploadAsOCIArtifact bool

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
