package internal

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

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
