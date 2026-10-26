package tar

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// VerifyGraph reads every node reachable from desc and checks it against its digest, so a
// matching root cannot vouch for substituted layers.
func VerifyGraph(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) error {
	return verifyGraph(ctx, fetcher, desc, map[digest.Digest]struct{}{})
}

func verifyGraph(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor, seen map[digest.Digest]struct{}) error {
	if _, ok := seen[desc.Digest]; ok {
		return nil
	}
	seen[desc.Digest] = struct{}{}

	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return fmt.Errorf("fetching %s failed: %w", desc.Digest, err)
	}
	vr := content.NewVerifyReader(rc, desc)
	_, err = io.Copy(io.Discard, vr)
	if err == nil {
		err = vr.Verify()
	}
	if err = errors.Join(err, rc.Close()); err != nil {
		return fmt.Errorf("verifying %s failed: %w", desc.Digest, err)
	}

	successors, err := content.Successors(ctx, fetcher, desc)
	if err != nil {
		return fmt.Errorf("reading successors of %s failed: %w", desc.Digest, err)
	}
	for _, successor := range successors {
		if err := verifyGraph(ctx, fetcher, successor, seen); err != nil {
			return err
		}
	}
	return nil
}
