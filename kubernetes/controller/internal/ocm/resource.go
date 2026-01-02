package ocm

import (
	"errors"
	"fmt"

	ocmctx "ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/tools/signing"
)

// VerifyResource verifies the resource digest with the digest from the component version access and component descriptor.
func VerifyResource(access ocmctx.ResourceAccess, cv ocmctx.ComponentVersionAccess) error {
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
