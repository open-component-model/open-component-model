package ocm

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// IsLocalResource reports whether res is stored with its component version and is read
// through the component version repository.
func IsLocalResource(res *descriptor.Resource) bool {
	return v2.IsLocalBlob(res.Access) || ociaccess.IsRelativeOCIReference(res.Access)
}

// VerifyLocalResource checks the content of res, a local resource of desc, against the
// digest res records. It is only downloaded if the repository cannot check it against
// what it is stored under (see [repository.VerifyLocalResource]).
func VerifyLocalResource(ctx context.Context, resolver *resolution.Resolver, opts *resolution.Options, desc *descriptor.Descriptor, res *descriptor.Resource) error {
	repoResolver, err := resolver.RepositoryResolver(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to create repository resolver: %w", err)
	}
	name, version := desc.Component.Name, desc.Component.Version
	repo, err := repoResolver.GetComponentVersionRepositoryForComponent(ctx, name, version)
	if err != nil {
		return fmt.Errorf("failed to get repository for component %s:%s: %w", name, version, err)
	}
	if err := repository.VerifyLocalResource(ctx, repo, name, version, res); err != nil {
		return fmt.Errorf("failed to verify local resource %q: %w", res.Name, err)
	}
	return nil
}

// VerifyResource verifies and processes the resource digest using the appropriate digest processor plugin.
func VerifyResource(ctx context.Context, pm *manager.PluginManager, resource *descriptor.Resource, cfg *configuration.Configuration) (*descriptor.Resource, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("processing resource digest")

	if resource.Access == nil {
		return nil, fmt.Errorf("resource %q has no access and cannot be verified", resource.Name)
	}
	digestProcessor, err := pm.DigestProcessorRegistry.GetPlugin(ctx, resource.Access)
	if err != nil {
		return nil, fmt.Errorf("resource %q of access type %s cannot be verified, set verificationPolicy Never to skip verification: %w",
			resource.Name, resource.Access.GetType(), err)
	}

	var creds runtime.Typed
	if cfg != nil {
		id, err := digestProcessor.GetResourceDigestProcessorCredentialConsumerIdentity(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("failed getting digest processor identity: %w", err)
		}

		credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: pm,
			Logger:        &logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed creating credential graph: %w", err)
		}

		creds, err = credGraph.Resolve(ctx, id)
		if err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("failed resolving credentials for digest processor: %w", err)
		}
	}

	// Process resource digest will also verify the digest if already present
	digestResource, err := digestProcessor.ProcessResourceDigest(ctx, resource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed processing resource digest: %w", err)
	}

	return digestResource, nil
}

// ResolveReferencePath walks a reference path from a parent component version to a final component version.
// It returns the final descriptor and repository spec.
// Each referenced component version is integrity-checked against the digest of its reference, if one is set.
func ResolveReferencePath(
	ctx context.Context,
	resolver *resolution.Resolver,
	parentDesc *descriptor.Descriptor,
	referencePath []runtime.Identity,
	opts *resolution.Options,
) (*descriptor.Descriptor, runtime.Typed, error) {
	logger := log.FromContext(ctx)

	if len(referencePath) == 0 {
		return parentDesc, opts.RepositorySpec, nil
	}

	currentDesc := parentDesc
	var errsNotSafelyDigestible error
	for i, refIdentity := range referencePath {
		logger.V(1).Info("resolving reference", "step", i+1, "identity", refIdentity)

		matchedRef := findMatchingReference(currentDesc, refIdentity)
		if matchedRef == nil {
			return nil, nil, fmt.Errorf("component reference with identity %v not found in component %s:%s at reference path step %d",
				refIdentity, currentDesc.Component.Name, currentDesc.Component.Version, i+1)
		}

		refDesc, err := resolver.GetComponentVersion(ctx, opts,
			resolution.Verification{Digest: extractDigest(matchedRef)},
			matchedRef.Component, matchedRef.Version)
		if err != nil {
			if !errors.Is(err, resolution.ErrNotSafelyDigestible) {
				return nil, nil, fmt.Errorf("failed to get referenced component version %s:%s: %w",
					matchedRef.Component, matchedRef.Version, err)
			}

			errsNotSafelyDigestible = errors.Join(errsNotSafelyDigestible, err)
		}

		currentDesc = refDesc
	}

	return currentDesc, opts.RepositorySpec, errsNotSafelyDigestible
}

func findMatchingReference(desc *descriptor.Descriptor, identity runtime.Identity) *descriptor.Reference {
	for j, ref := range desc.Component.References {
		if identity.Match(ref.ToIdentity(), IdentityFuncIgnoreVersion()) {
			return &desc.Component.References[j]
		}
	}
	return nil
}

func extractDigest(ref *descriptor.Reference) *v2.Digest {
	if ref.Digest.Value != "" && ref.Digest.HashAlgorithm != "" && ref.Digest.NormalisationAlgorithm != "" {
		return &v2.Digest{
			HashAlgorithm:          ref.Digest.HashAlgorithm,
			Value:                  ref.Digest.Value,
			NormalisationAlgorithm: ref.Digest.NormalisationAlgorithm,
		}
	}
	return nil
}

// IdentityFuncIgnoreVersion is a custom identity matching function that ignores the "version" field if it is not set.
func IdentityFuncIgnoreVersion() runtime.IdentityMatchingChainFn {
	return func(i, o runtime.Identity) bool {
		version, ok := i["version"]
		if !ok || version == "" {
			delete(o, "version")
		}
		return runtime.IdentityEqual(i, o)
	}
}

// AddressableIdentities returns, for each element of ids, an identity that
// matches exactly that element under IdentityFuncIgnoreVersion, so the first
// match taken by ResolveReferencePath and the Resource controller is the only
// one. It prefers the identity without its version, which keeps matching when
// the element's version changes, and falls back to the full identity. An
// element that no identity selects on its own, such as one of two equal
// identities, gets nil. ids must be all elements the identity is matched
// against, not a subset.
func AddressableIdentities(ids []runtime.Identity) []runtime.Identity {
	stripped := make([]runtime.Identity, len(ids))
	strippedKeys := make([]string, len(ids))
	fullKeys := make([]string, len(ids))
	strippedCount := make(map[string]int, len(ids))
	fullCount := make(map[string]int, len(ids))
	for i, id := range ids {
		stripped[i] = withoutVersion(id)
		strippedKeys[i] = identityKey(stripped[i])
		fullKeys[i] = identityKey(id)
		strippedCount[strippedKeys[i]]++
		fullCount[fullKeys[i]]++
	}

	out := make([]runtime.Identity, len(ids))
	for i, id := range ids {
		switch {
		case strippedCount[strippedKeys[i]] == 1:
			out[i] = stripped[i]
		// Without a version the full identity is the stripped one, which
		// IdentityFuncIgnoreVersion also matches against versioned siblings.
		case id[descriptor.IdentityAttributeVersion] != "" && fullCount[fullKeys[i]] == 1:
			out[i] = id.Clone()
		}
	}
	return out
}

// identityKey encodes id so that two identities share a key exactly when they
// are equal. runtime.Identity.String joins key=value pairs with commas, so
// values containing "," or "=" can make different identities collide.
func identityKey(id runtime.Identity) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(id)) {
		// Quoting escapes the separators, so the encoding is unambiguous.
		b.WriteString(strconv.Quote(k))
		b.WriteByte('=')
		b.WriteString(strconv.Quote(id[k]))
		b.WriteByte(',')
	}
	return b.String()
}

func withoutVersion(id runtime.Identity) runtime.Identity {
	s := id.Clone()
	if s == nil {
		s = runtime.Identity{}
	}
	delete(s, descriptor.IdentityAttributeVersion)
	return s
}
