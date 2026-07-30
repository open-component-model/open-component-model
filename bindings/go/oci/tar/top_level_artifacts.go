package tar

import (
	"context"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// TopLevelArtifacts returns the main top-level artifacts from a list of
// candidates. A candidate is excluded when it is a referrer (declares a
// subject) or when another candidate contains it as a successor. The remaining
// candidates are returned in input order.
func TopLevelArtifacts(ctx context.Context, fetcher content.Fetcher, candidates []ociImageSpecV1.Descriptor) []ociImageSpecV1.Descriptor {
	var mu sync.Mutex
	excluded := make(map[digest.Digest]struct{}, len(candidates))

	var wg sync.WaitGroup
	wg.Add(len(candidates))
	for i := range candidates {
		go func() {
			defer wg.Done()
			subject, successors, err := extractSubjectAndSuccessors(ctx, fetcher, candidates[i])
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if subject != nil {
				excluded[candidates[i].Digest] = struct{}{}
				return
			}
			for _, s := range successors {
				excluded[s.Digest] = struct{}{}
			}
		}()
	}
	wg.Wait()

	topLevel := make([]ociImageSpecV1.Descriptor, 0, len(candidates))
	for _, artifact := range candidates {
		if _, drop := excluded[artifact.Digest]; drop {
			continue
		}
		topLevel = append(topLevel, artifact)
	}
	return topLevel
}
