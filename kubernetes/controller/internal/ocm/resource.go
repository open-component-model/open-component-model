package ocm

import (
	"context"
	"errors"
	"fmt"

	ocmctx "ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/compdesc"
	v1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"ocm.software/ocm/api/ocm/selectors"
	"ocm.software/ocm/api/ocm/tools/signing"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func GetResourceAccessForComponentVersion(
	ctx context.Context,
	session ocmctx.Session,
	cv ocmctx.ComponentVersionAccess,
	reference v1.ResourceReference,
	cdSet *Descriptors,
	resolver ocmctx.ComponentVersionResolver,
	skipVerification bool,
) (ocmctx.ResourceAccess, ocmctx.ComponentVersionAccess, error) {
	logger := log.FromContext(ctx)
	// Resolve resource resourceReference to get resource and its component descriptor
	resourceDesc, resourceCompDesc, err := compdesc.ResolveResourceReference(cv.GetDescriptor(), reference, compdesc.NewComponentVersionSet(cdSet.List...))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve resource reference: %w", err)
	}

	resourceCV, err := session.LookupComponentVersion(resolver, resourceCompDesc.GetName(), resourceCompDesc.GetVersion())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup component version for resource: %w", err)
	}

	resourceAccesses, err := resourceCV.SelectResources(selectors.Identity(resourceDesc.GetIdentity(resourceCompDesc.GetResources())))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to select resources: %w", err)
	}

	var resourceAccess ocmctx.ResourceAccess
	switch len(resourceAccesses) {
	case 0:
		return nil, nil, errors.New("no resources selected")
	case 1:
		resourceAccess = resourceAccesses[0]
	default:
		return nil, nil, errors.New("cannot determine the resource access unambiguously")
	}

	if !skipVerification {
		if err := verifyResource(resourceAccess, resourceCV); err != nil {
			return nil, nil, fmt.Errorf("failed to verify resource: %w", err)
		}
	} else {
		logger.V(1).Info("skipping resource verification")
	}

	return resourceAccess, resourceCV, nil
}

// verifyResource verifies the resource digest with the digest from the component version access and component descriptor.
func verifyResource(access ocmctx.ResourceAccess, cv ocmctx.ComponentVersionAccess) error {
	// Create data access
	accessMethod, err := access.AccessMethod()
	if err != nil {
		return fmt.Errorf("failed to create access method: %w", err)
	}

	// Add the component descriptor to the local verified store, so its digest will be compared with the digest from the
	// component version access
	store := signing.NewLocalVerifiedStore()
	store.Add(cv.GetDescriptor())

	ok, err := signing.VerifyResourceDigestByResourceAccess(cv, access, accessMethod.AsBlobAccess(), store)
	if !ok {
		if err != nil {
			return fmt.Errorf("verification failed: %w", err)
		}

		return errors.New("expected signature verification to be relevant, but it was not")
	}
	if err != nil {
		return fmt.Errorf("failed to verify resource digest: %w", err)
	}

	return nil
}
