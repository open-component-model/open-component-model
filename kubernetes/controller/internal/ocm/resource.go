package ocm

import (
	"context"
	"errors"
	"fmt"

	ocmctx "ocm.software/ocm/api/ocm"
	v1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"ocm.software/ocm/api/ocm/resourcerefs"
	"ocm.software/ocm/api/ocm/tools/signing"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func GetResourceAccessForComponentVersion(ctx context.Context, cv ocmctx.ComponentVersionAccess, reference v1.ResourceReference, resolver ocmctx.ComponentVersionResolver, skipVerification bool) (ocmctx.ResourceAccess, ocmctx.ComponentVersionAccess, error) {
	logger := log.FromContext(ctx)

	resAcc, cvAcc, err := resourcerefs.ResolveResourceReference(cv, reference, resolver)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve resource reference: %w", err)
	}

	if !skipVerification {
		if err := verifyResource(resAcc, cvAcc); err != nil {
			return nil, nil, fmt.Errorf("failed to verify resource: %w", err)
		}
	} else {
		logger.V(1).Info("skipping resource verification")
	}

	return resAcc, cvAcc, nil
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
