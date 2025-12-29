package internal

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"ocm.software/open-component-model/bindings/go/dag"
	dagsync "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

func BuildGraphDefinition(
	ctx context.Context,
	fromSpec *compref.Ref,
	toSpec runtime.Typed,
	repoProvider ocm.ComponentVersionRepositoryForComponentProvider,
	recursive bool,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	discoverer := &discoverer{
		recursive:         recursive,
		discoveredDigests: make(map[string]descriptorv2.Digest),
	}
	resolver := &resolver{
		repoProvider: repoProvider,
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
			return descriptor.ConvertFromV2Digest(&dig)
		},
	}

	dr := dagsync.NewGraphDiscoverer(&dagsync.GraphDiscovererOptions[string, *discoveryValue]{
		Roots:      []string{fromSpec.String()},
		Resolver:   resolver,
		Discoverer: discoverer,
	})

	if err := dr.Discover(ctx); err != nil {
		return nil, fmt.Errorf("recursive discovery failed: %w", err)
	}

	tgd := &transformv1alpha1.TransformationGraphDefinition{
		Environment: &runtime.Unstructured{
			Data: map[string]interface{}{
				"to": AsUnstructured(toSpec).Data,
			},
		},
	}

	g := dr.Graph()
	err := g.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
		for _, v := range d.Vertices {
			val := v.Attributes[dagsync.AttributeValue].(*discoveryValue)
			ref := val.Ref

			id := identityToTransformationID(ref.Identity())

			tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
				TransformationMeta: meta.TransformationMeta{
					Type: ChooseGetType(ref.Repository),
					ID:   id + "Download",
				},
				Spec: &runtime.Unstructured{Data: map[string]interface{}{
					"repository": AsUnstructured(ref.Repository).Data,
					"component":  ref.Component,
					"version":    ref.Version,
				}},
			})

			tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
				TransformationMeta: meta.TransformationMeta{
					Type: ChooseAddType(toSpec),
					ID:   id + "Upload",
				},
				Spec: &runtime.Unstructured{Data: map[string]interface{}{
					"repository": AsUnstructured(toSpec).Data,
					"descriptor": fmt.Sprintf("${%sDownload.output.descriptor}", id),
				}},
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return tgd, nil
}

type discoveryValue struct {
	Ref        *compref.Ref
	Descriptor *descriptorv2.Descriptor
	Digest     *descriptorv2.Digest
}

type resolver struct {
	repoProvider   ocm.ComponentVersionRepositoryForComponentProvider
	expectedDigest func(id runtime.Identity) *descriptor.Digest
}

func (r *resolver) Resolve(ctx context.Context, key string) (*discoveryValue, error) {
	ref, err := compref.Parse(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference %q: %w", key, err)
	}

	repo, err := r.repoProvider.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx, ref.Component, ref.Version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %w", ref.Component, ref.Version, err)
	}

	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	if err != nil {
		return nil, fmt.Errorf("failed converting component version to v2: %w", err)
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
		Descriptor: v2desc,
	}, nil
}

type discoverer struct {
	mu        sync.Mutex
	recursive bool

	discoveredDigests map[string]descriptorv2.Digest
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
			d.discoveredDigests[ref.ToIdentity().String()] = ref.Digest
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
