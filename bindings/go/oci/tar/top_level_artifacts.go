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
//
// Each candidate is fetched and decoded once, in parallel, to determine both
// its subject and its containment successors. A fetch or decode error for a
// candidate is treated as "not a referrer, contributes no edges" so a transient
// failure cannot silently drop a real top-level artifact.
func TopLevelArtifacts(ctx context.Context, fetcher content.Fetcher, candidates []ociImageSpecV1.Descriptor) []ociImageSpecV1.Descriptor {
	var mu sync.Mutex
	excluded := make(map[digest.Digest]struct{}, len(candidates))

	var wg sync.WaitGroup
	wg.Add(len(candidates))
	for i := range candidates {
		go func() {
			defer wg.Done()
			subject, successors, err := classify(ctx, fetcher, candidates[i])
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
