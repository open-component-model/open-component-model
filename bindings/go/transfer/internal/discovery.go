package internal

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

type discoveryValue struct {
	Descriptor       *descriptor.Descriptor
	SourceRepository runtime.Typed
}

type resolver struct {
	repoResolver   resolvers.ComponentVersionRepositoryResolver
	expectedDigest func(id runtime.Identity) *descriptor.Digest
}

func (r *resolver) Resolve(ctx context.Context, key string) (*discoveryValue, error) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid key format %q: expected component:version", key)
	}
	component, version := parts[0], parts[1]

	repoSpec, err := r.repoResolver.GetRepositorySpecificationForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting repository spec for component %s:%s: %w", component, version, err)
	}

	repo, err := r.repoResolver.GetComponentVersionRepositoryForSpecification(ctx, repoSpec)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository for spec %v: %w", repoSpec, err)
	}

	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %w", component, version, err)
	}

	if expected := r.expectedDigest(desc.Component.ToIdentity()); expected != nil {
		if err := signing.VerifyDigestMatchesDescriptor(
			ctx, desc, descriptor.Signature{Digest: *expected}, slog.Default(),
		); err != nil {
			return nil, fmt.Errorf("failed verifying resolved descriptor matches expected digest: %w", err)
		}
	}

	return &discoveryValue{
		Descriptor:       desc,
		SourceRepository: repoSpec,
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
		key := ref.Component + ":" + ref.Version

		if ref.Digest.Value != "" {
			d.mu.Lock()
			d.discoveredDigests[ref.ToComponentIdentity().String()] = ref.Digest
			d.mu.Unlock()
		}
		children = append(children, key)
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
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		words = append(words, strings.FieldsFunc(id[k], func(r rune) bool {
			return slices.Contains(toWordRunes, r)
		})...)
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
