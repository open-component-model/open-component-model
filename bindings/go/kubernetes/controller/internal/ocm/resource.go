package ocm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var ErrPluginNotFound = errors.New("digest processor plugin not found")

// LocalResourceProvider fetches a by-value (localBlob) resource blob from its owning
// component version, so its content can be verified against the resource digest.
type LocalResourceProvider interface {
	GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error)
}

// IsLocalBlob reports whether the access describes a component-local blob.
func IsLocalBlob(access runtime.Typed) bool {
	if access == nil {
		return false
	}
	if _, ok := access.(*descriptor.LocalBlob); ok {
		return true
	}
	name := access.GetType().Name
	return name == descriptor.LocalBlobAccessType || name == descriptor.LegacyLocalBlobAccessType
}

// IsExcludedFromSignature reports whether the resource digest carries the explicit
// marker that keeps the resource content out of the component signature. Such
// resources have no digest to verify against, by author intent, so verification is
// not applicable rather than failed.
func IsExcludedFromSignature(d *descriptor.Digest) bool {
	if d == nil {
		return false
	}
	return strings.EqualFold(d.HashAlgorithm, descriptor.NoDigest) ||
		strings.EqualFold(d.NormalisationAlgorithm, descriptor.ExcludeFromSignature)
}

// VerifyLocalBlob verifies a by-value (localBlob) resource by downloading its content
// from the owning component version and hashing it against the resource digest. A
// localBlob has no digest processor; its integrity is established on download via the
// repository's content-addressed store plus the resource digest. A resource without a
// usable digest cannot be verified and is rejected.
func VerifyLocalBlob(ctx context.Context, repo LocalResourceProvider, component, version string, resource *descriptor.Resource) error {
	if resource.Digest == nil || resource.Digest.Value == "" || resource.Digest.HashAlgorithm == "" {
		return fmt.Errorf("resource %q has no digest; cannot verify integrity", resource.Name)
	}

	b, _, err := repo.GetLocalResource(ctx, component, version, resource.ToIdentity())
	if err != nil {
		return fmt.Errorf("failed to get local resource %q: %w", resource.Name, err)
	}
	// The blob may own resources (e.g. a temporary file); close it on every path, not just
	// via the reader. Closing the reader releases the read handle but not the blob itself.
	if closer, ok := b.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	// VerifyDownload returns a blob that hashes its content against resource.Digest as it
	// is read. The mismatch only surfaces once the content is fully read and closed, so the
	// content must be drained to actually perform the check.
	verifying, err := ocmrepository.VerifyDownload(ctx, resource, b)
	if err != nil {
		return fmt.Errorf("failed to prepare verification for resource %q: %w", resource.Name, err)
	}

	rc, err := verifying.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to read local resource %q: %w", resource.Name, err)
	}
	if _, err := io.Copy(io.Discard, rc); err != nil {
		_ = rc.Close()
		return fmt.Errorf("failed to verify local resource %q: %w", resource.Name, err)
	}
	if err := rc.Close(); err != nil {
		return fmt.Errorf("digest verification failed for resource %q: %w", resource.Name, err)
	}

	return nil
}

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
