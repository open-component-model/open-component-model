package ocm

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/kubernetes/controller/pkg/configuration"
)

var ErrPluginNotFound = errors.New("digest processor plugin not found")

// VerifyResource verifies and processes the resource digest using the appropriate digest processor plugin.
func VerifyResource(ctx context.Context, pm *manager.PluginManager, resource *descriptor.Resource, cfg *configuration.Configuration) (*descriptor.Resource, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("processing resource digest")

	digestProcessor, err := pm.DigestProcessorRegistry.GetPlugin(ctx, resource.Access)
	if err != nil {
		// Return the resource along with the error to allow further handling if needed
		// (Currently, we just log the error and continue without digest verification because some resources may not
		// have digest processors yet)
		return resource, errors.Join(ErrPluginNotFound, err)
	}

	var creds map[string]string
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

		creds, err = credGraph.Resolve(ctx, id) //nolint:staticcheck // SA1019: tracked migration to ResolveTyped in ocm-project#702
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
// The baseOpts are used as a template for each resolution step; only the Digest field is overridden per reference.
func ResolveReferencePath(
	ctx context.Context,
	resolver *resolution.Resolver,
	parentDesc *descriptor.Descriptor,
	referencePath []runtime.Identity,
	baseOpts *resolution.RepositoryOptions,
) (*descriptor.Descriptor, runtime.Typed, error) {
	logger := log.FromContext(ctx)

	if len(referencePath) == 0 {
		return parentDesc, baseOpts.RepositorySpec, nil
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

		stepOpts := *baseOpts
		stepOpts.Digest = extractDigest(matchedRef)

		refRepo, err := resolver.NewCacheBackedRepository(ctx, &stepOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create cache-backed repository for reference: %w", err)
		}

		refDesc, err := refRepo.GetComponentVersion(ctx, matchedRef.Component, matchedRef.Version)
		if err != nil {
			if !errors.Is(err, workerpool.ErrNotSafelyDigestible) {
				return nil, nil, fmt.Errorf("failed to get referenced component version %s:%s: %w",
					matchedRef.Component, matchedRef.Version, err)
			}

			errsNotSafelyDigestible = errors.Join(errsNotSafelyDigestible, err)
		}

		currentDesc = refDesc
	}

	return currentDesc, baseOpts.RepositorySpec, errsNotSafelyDigestible
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
