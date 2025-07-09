package tar

import (
	"context"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2/content"
)

// TopLevelArtifacts returns the top-level artifacts from a list of candidates.
// An artifact is considered a top-level artifact if it is not referenced by any other artifact.
// If there is only one artifact in the candidates, it is automatically considered a top-level artifact.
// It uses content.Successors to find successors of each artifact.
// The function returns a slice of top-level artifacts.
func TopLevelArtifacts(ctx context.Context, fetcher content.Fetcher, candidates []ociImageSpecV1.Descriptor) []ociImageSpecV1.Descriptor {
	// If there's only one artifact, it's automatically a top-level artifact
	if len(candidates) <= 1 {
		return candidates
	}

	// Build a set of all referenced digests
	var lock sync.Mutex
	referenced := make(map[digest.Digest]struct{}, len(candidates))

	eg, ctx := errgroup.WithContext(ctx)
	// For each artifact in the index, find all the artifacts it references
	for _, artifact := range candidates {
		// Get the successors (referenced artifacts) for this artifact
		eg.Go(func() error {
			successors, err := content.Successors(ctx, fetcher, artifact)
			if err != nil {
				// If we can't get successors, skip this artifact
				return nil
			}

			// Mark all successors as referenced
			for _, successor := range successors {
				lock.Lock()
				referenced[successor.Digest] = struct{}{}
				lock.Unlock()
			}
			return nil
		})
	}

	_ = eg.Wait() // Wait for all goroutines to finish

	// Return artifacts that are not referenced by any other artifact
	topLevel := make([]ociImageSpecV1.Descriptor, 0, len(referenced))
	for _, artifact := range candidates {
		if _, referenced := referenced[artifact.Digest]; !referenced {
			topLevel = append(topLevel, artifact)
		}
	}

	return topLevel
}
