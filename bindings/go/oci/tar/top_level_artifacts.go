package tar

import (
	"context"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
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
	var mu sync.Mutex
	referenced := make(map[digest.Digest]struct{}, len(candidates))

	// resolveReferences is a function that finds all successors of an artifact
	// and adds their digests to the reference cache.
	resolveReferences := func(artifact ociImageSpecV1.Descriptor) {
		if successors, err := content.Successors(ctx, fetcher, artifact); err == nil {
			mu.Lock()
			defer mu.Unlock()
			for _, successor := range successors {
				referenced[successor.Digest] = struct{}{}
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(len(candidates))
	// For each artifact in the index, find all the artifacts it references
	for i := range candidates {
		go func() {
			defer wg.Done()
			resolveReferences(candidates[i])
		}()
	}
	wg.Wait()

	// Return artifacts that are not referenced by any other artifact
	// Pre-allocate with worst-case capacity (all candidates could be top-level)
	topLevel := make([]ociImageSpecV1.Descriptor, 0, len(candidates))
	for _, artifact := range candidates {
		if _, isReferenced := referenced[artifact.Digest]; !isReferenced {
			topLevel = append(topLevel, artifact)
		}
	}

	return topLevel
}
