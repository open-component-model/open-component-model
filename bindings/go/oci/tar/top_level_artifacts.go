package tar

import (
	"context"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// TopLevelArtifacts returns the top-level artifacts from a list of candidates.
// An artifact is top-level when it is not contained by another candidate (not
// one of its successors).
//
// The Subject edge is deliberately NOT treated as containment: a referrer points
// "back" at its subject, so counting that edge would mark the subject as
// referenced and wrongly hide it, surfacing the referrer instead. Referrers
// themselves are still returned (nothing contains them); callers that want only
// the main content artifacts must exclude referrers separately — see
// [CloseableReadOnlyStore.MainArtifacts].
//
// If there is only one candidate, it is returned as-is.
func TopLevelArtifacts(ctx context.Context, fetcher content.Fetcher, candidates []ociImageSpecV1.Descriptor) []ociImageSpecV1.Descriptor {
	// If there's only one artifact, it's automatically a top-level artifact
	if len(candidates) <= 1 {
		return candidates
	}

	// Build a set of all contained digests
	var mu sync.Mutex
	referenced := make(map[digest.Digest]struct{}, len(candidates))

	// resolveReferences records an artifact's containment successors (subject excluded).
	resolveReferences := func(artifact ociImageSpecV1.Descriptor) {
		successors, err := successorsWithoutSubject(ctx, fetcher, artifact)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, successor := range successors {
			referenced[successor.Digest] = struct{}{}
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

	// Return artifacts that are not contained by any other artifact
	// Pre-allocate with worst-case capacity (all candidates could be top-level)
	topLevel := make([]ociImageSpecV1.Descriptor, 0, len(candidates))
	for _, artifact := range candidates {
		if _, isReferenced := referenced[artifact.Digest]; !isReferenced {
			topLevel = append(topLevel, artifact)
		}
	}

	return topLevel
}
