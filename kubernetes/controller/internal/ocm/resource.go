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
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/configuration"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
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
func ResolveReferencePath(
	ctx context.Context,
	resolver *resolution.Resolver,
	signingRegistry *signinghandler.SigningRegistry,
	parentDesc *descriptor.Descriptor,
	parentRepoSpec runtime.Typed,
	referencePath []runtime.Identity,
	configs []v1alpha1.OCMConfiguration,
	reqInfo workerpool.RequesterInfo,
) (*descriptor.Descriptor, runtime.Typed, error) {
	logger := log.FromContext(ctx)

	if len(referencePath) == 0 {
		return parentDesc, parentRepoSpec, nil
	}

	currentDesc := parentDesc
	currentRepoSpec := parentRepoSpec
	var errsNotSafelyDigestible error
	for i, refIdentity := range referencePath {
		logger.V(1).Info("resolving reference", "step", i+1, "identity", refIdentity)

		var matchedRef *descriptor.Reference
		for j, ref := range currentDesc.Component.References {
			refIdent := ref.ToIdentity()
			if refIdentity.Match(refIdent, IdentityFuncIgnoreVersion()) {
				matchedRef = &currentDesc.Component.References[j]
				break
			}
		}

		if matchedRef == nil {
			return nil, nil, fmt.Errorf("component reference with identity %v not found in component %s:%s at reference path step %d",
				refIdentity, currentDesc.Component.Name, currentDesc.Component.Version, i+1)
		}

		// If the reference contains a digest spec, we pass it to the cache-backed repository, so it is used for the
		// cache-key creation and digest integrity check in the resolution service. This is the digest of the
		// referenced component from the component reference of the parent component.
		var refDigest *v2.Digest
		if matchedRef.Digest.Value != "" && matchedRef.Digest.HashAlgorithm != "" && matchedRef.Digest.NormalisationAlgorithm != "" {
			refDigest = &v2.Digest{
				HashAlgorithm:          matchedRef.Digest.HashAlgorithm,
				Value:                  matchedRef.Digest.Value,
				NormalisationAlgorithm: matchedRef.Digest.NormalisationAlgorithm,
			}
		}

		refRepo, err := resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
			RepositorySpec:    currentRepoSpec,
			OCMConfigurations: configs,
			Namespace:         reqInfo.NamespacedName.Namespace,
			SigningRegistry:   signingRegistry,
			Digest:            refDigest,
			RequesterFunc: func() workerpool.RequesterInfo {
				return reqInfo
			},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create cache-backed repository for reference: %w", err)
		}

		refDesc, err := refRepo.GetComponentVersion(ctx, matchedRef.Component, matchedRef.Version)
		if err != nil {
			if !errors.Is(err, workerpool.ErrNotSafelyDigestible) {
				return nil, nil, fmt.Errorf("failed to get referenced component version %s:%s: %w",
					matchedRef.Component, matchedRef.Version, err)
			}

			// GetComponentVersion can return a ErrNotSafelyDigestible error that needs to create an error event
			// on the CR without terminating the reconciliation. Hence, we gather any ErrNotSafelyDigestible error to
			// return it to the caller of this function.
			errsNotSafelyDigestible = errors.Join(errsNotSafelyDigestible, err)
		}

		currentDesc = refDesc
	}

	return currentDesc, currentRepoSpec, errsNotSafelyDigestible
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
