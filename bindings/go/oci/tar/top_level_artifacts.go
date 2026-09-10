package tar

import (
	"context"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// AnnotationLayoutRoot names the artifact a layout was built for, on that
// artifact's index.json entry, with the value "true".
const AnnotationLayoutRoot = "software.ocm.layout.root"

// TopLevelArtifacts picks the artifacts a layout is actually about, out of
// everything its index lists.
func TopLevelArtifacts(ctx context.Context, fetcher content.Fetcher, candidates []ociImageSpecV1.Descriptor) []ociImageSpecV1.Descriptor {
	if root, ok := markedRoot(candidates); ok {
		return []ociImageSpecV1.Descriptor{root}
	}

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

// markedRoot returns the candidate carrying [AnnotationLayoutRoot]
func markedRoot(candidates []ociImageSpecV1.Descriptor) (ociImageSpecV1.Descriptor, bool) {
	var root ociImageSpecV1.Descriptor
	var found int
	for _, candidate := range candidates {
		if candidate.Annotations[AnnotationLayoutRoot] == "true" {
			root, found = candidate, found+1
		}
	}
	return root, found == 1
}
