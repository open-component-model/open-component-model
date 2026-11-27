package attestation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/repository"
)

func TestDedup_ByContentDigest(t *testing.T) {
	withDigest := func(digest, data string) repository.Attestation {
		return repository.Attestation{ContentDigest: digest, Data: []byte(data)}
	}
	in := []repository.Attestation{
		withDigest("sha256:aaaa", "vex-bytes"),  // first of group aaaa
		withDigest("sha256:bbbb", "sbom-bytes"), // unique
		withDigest("sha256:aaaa", "vex-bytes"),  // same digest, dropped
		{Data: []byte("no-digest")},             // no ContentDigest -> hashed
		{Data: []byte("no-digest")},             // same bytes, dropped via hash fallback
	}

	kept, skipped := Dedup(in)

	assert.Len(t, kept, 3, "two digest groups plus the hash-fallback group")
	assert.Len(t, skipped, 2, "the two byte-identical copies are dropped")

	// First occurrence of each distinct content is kept, in input order.
	assert.Equal(t, "sha256:aaaa", kept[0].ContentDigest)
	assert.Equal(t, "sha256:bbbb", kept[1].ContentDigest)
	assert.Equal(t, []byte("no-digest"), kept[2].Data, "content with no digest is deduplicated by hashing its bytes")
}
