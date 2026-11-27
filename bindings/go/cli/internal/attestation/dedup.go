package attestation

import (
	"crypto/sha256"
	"encoding/hex"

	"ocm.software/open-component-model/bindings/go/repository"
)

// Dedup collapses attestations carrying byte-identical documents to a single entry, keyed
// by ContentDigest (the digest of the document bytes) and falling back to a hash of the
// fetched Data when a source exposes no content digest. The first occurrence in input order
// is kept, so the result is deterministic and matches the duplicate grouping shown by
// "ocm get attestation". kept and skipped partition the input; skipped holds the dropped
// copies so the caller can report them.
func Dedup(atts []repository.Attestation) (kept, skipped []repository.Attestation) {
	seen := make(map[string]struct{}, len(atts))
	for _, a := range atts {
		key := a.ContentDigest
		if key == "" {
			sum := sha256.Sum256(a.Data)
			key = "sha256:" + hex.EncodeToString(sum[:])
		}
		if _, ok := seen[key]; ok {
			skipped = append(skipped, a)
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, a)
	}
	return kept, skipped
}
