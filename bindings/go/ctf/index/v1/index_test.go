package v1

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestNewIndex_Empty(t *testing.T) {
	idx := NewIndex()
	arts := idx.GetArtifacts()
	if len(arts) != 0 {
		t.Fatalf("expected empty artifacts, got %d", len(arts))
	}
}

func TestAddArtifact_AddAndGet(t *testing.T) {
	idx := NewIndex()
	a := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "v1",
		Digest:     "sha256:abc",
		MediaType:  "type1",
	}
	idx.AddArtifact(a)
	arts := idx.GetArtifacts()
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0] != a {
		t.Errorf("artifact mismatch: got %+v, want %+v", arts[0], a)
	}
}

func TestAddArtifact_RetagScenario(t *testing.T) {
	idx := NewIndex()
	a1 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "v1",
		Digest:     "sha256:abc",
	}
	a2 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "v1",
		Digest:     "sha256:def",
	}
	idx.AddArtifact(a1)
	idx.AddArtifact(a2)
	arts := idx.GetArtifacts()
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact (old entry deleted during retag), got %d", len(arts))
	}
	art := arts[0]
	if art.Tag != "v1" {
		t.Errorf("expected tag v1, got %q", art.Tag)
	}
	if art.Digest != "sha256:def" {
		t.Errorf("expected new digest sha256:def, got %s", art.Digest)
	}
}

func TestAddArtifact_TagScenario(t *testing.T) {
	idx := NewIndex()
	a1 := ArtifactMetadata{
		Repository: "repo1",
		Digest:     "sha256:abc",
	}
	a2 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "v1",
		Digest:     "sha256:abc",
	}
	idx.AddArtifact(a1)
	idx.AddArtifact(a2)
	arts := idx.GetArtifacts()
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].Tag != "v1" {
		t.Errorf("expected tag to be updated to v1, got %q", arts[0].Tag)
	}
}

func TestEncodeDecodeIndex(t *testing.T) {
	idx := NewIndex()
	idx.AddArtifact(ArtifactMetadata{
		Repository: "repo1",
		Tag:        "v1",
		Digest:     "sha256:abc",
	})
	data, err := Encode(idx)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeIndex(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	arts := decoded.GetArtifacts()
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact after decode, got %d", len(arts))
	}
	if arts[0].Repository != "repo1" || arts[0].Tag != "v1" || arts[0].Digest != "sha256:abc" {
		t.Errorf("decoded artifact mismatch: %+v", arts[0])
	}
}

func TestDecodeIndex_SchemaVersionMismatch(t *testing.T) {
	// schema version 999 is not supported
	data := []byte(`{"schemaVersion":999,"artifacts":[]}`)
	_, err := DecodeIndex(bytes.NewReader(data))
	if !errors.Is(err, ErrSchemaVersionMismatch) {
		t.Fatalf("expected schema version mismatch error, got %v", err)
	}
}

func TestAddArtifact_MultipleTagsSameDigest(t *testing.T) {
	idx := NewIndex()
	a1 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "v1.0.0",
		Digest:     "sha256:abc",
	}
	a2 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "latest",
		Digest:     "sha256:abc",
	}
	a3 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "stable",
		Digest:     "sha256:abc",
	}
	idx.AddArtifact(a1)
	idx.AddArtifact(a2)
	idx.AddArtifact(a3)
	arts := idx.GetArtifacts()
	if len(arts) != 3 {
		t.Fatalf("expected 3 artifacts with different tags, got %d", len(arts))
	}

	// Verify all three tags exist
	tags := make(map[string]bool)
	for _, art := range arts {
		if art.Digest == "sha256:abc" {
			tags[art.Tag] = true
		}
	}
	for _, expectedTag := range []string{"v1.0.0", "latest", "stable"} {
		if !tags[expectedTag] {
			t.Errorf("expected tag %q to exist", expectedTag)
		}
	}
}

func TestAddArtifact_DuplicateEntry(t *testing.T) {
	t.Run("tagged duplicate", func(t *testing.T) {
		// Adding the exact same entry twice should not create duplicates
		idx := NewIndex()
		a := ArtifactMetadata{
			Repository: "repo1",
			Tag:        "v1",
			Digest:     "sha256:abc",
		}
		idx.AddArtifact(a)
		idx.AddArtifact(a) // duplicate
		arts := idx.GetArtifacts()
		if len(arts) != 1 {
			t.Fatalf("expected 1 artifact (no duplicates), got %d", len(arts))
		}
	})

	t.Run("untagged duplicate", func(t *testing.T) {
		// Adding the same untagged artifact twice should not create duplicates
		idx := NewIndex()
		a := ArtifactMetadata{
			Repository: "repo1",
			Tag:        "", // untagged
			Digest:     "sha256:abc",
		}
		idx.AddArtifact(a)
		idx.AddArtifact(a) // duplicate
		arts := idx.GetArtifacts()
		if len(arts) != 1 {
			t.Fatalf("expected 1 untagged artifact (no duplicates), got %d", len(arts))
		}
		if arts[0].Tag != "" {
			t.Errorf("expected untagged artifact, got tag %q", arts[0].Tag)
		}
	})
}

func TestAddArtifact_CrossRepositoryIsolation(t *testing.T) {
	idx := NewIndex()
	// Same tag and digest in different repositories should not interfere
	idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "latest", Digest: "sha256:abc"})
	idx.AddArtifact(ArtifactMetadata{Repository: "repo2", Tag: "latest", Digest: "sha256:abc"})

	arts := idx.GetArtifacts()
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts (one per repo), got %d", len(arts))
	}
}

func TestEncodeDecodeIndex_MultipleTagsSameDigest(t *testing.T) {
	idx := NewIndex()
	idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "v1.0.0", Digest: "sha256:abc"})
	idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "latest", Digest: "sha256:abc"})

	data, err := Encode(idx)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeIndex(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	arts := decoded.GetArtifacts()
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts after decode, got %d", len(arts))
	}

	tags := make(map[string]bool)
	for _, art := range arts {
		tags[art.Tag] = true
	}
	if !tags["v1.0.0"] || !tags["latest"] {
		t.Errorf("expected both tags to persist after encode/decode, got %v", tags)
	}
}

func TestAddArtifact_RetagFromUntaggedToLatest(t *testing.T) {
	idx := NewIndex()
	a1 := ArtifactMetadata{
		Repository: "repo1",
		Digest:     "sha256:new",
	}
	a2 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "latest",
		Digest:     "sha256:old",
	}
	a3 := ArtifactMetadata{
		Repository: "repo1",
		Tag:        "latest",
		Digest:     "sha256:new",
	}

	idx.AddArtifact(a1)
	idx.AddArtifact(a2)
	idx.AddArtifact(a3)

	arts := idx.GetArtifacts()
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact (old entry deleted), got %d", len(arts))
	}

	art := arts[0]
	if art.Tag != "latest" {
		t.Errorf("expected tag 'latest', got %q", art.Tag)
	}
	if art.Digest != "sha256:new" {
		t.Errorf("expected digest sha256:new, got %s", art.Digest)
	}
}
func TestAddArtifact_MultipleUntaggedArtifacts(t *testing.T) {
	idx := NewIndex()
	// Add multiple untagged artifacts with different digests - all should be stored
	idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Digest: "sha256:abc"})
	idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Digest: "sha256:def"})
	idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Digest: "sha256:ghi"})

	arts := idx.GetArtifacts()
	if len(arts) != 3 {
		t.Fatalf("expected 3 untagged artifacts, got %d", len(arts))
	}

	// Verify all are untagged and have correct digests
	digests := make(map[string]bool)
	for _, art := range arts {
		if art.Tag != "" {
			t.Errorf("expected all artifacts to be untagged, but found tag %q", art.Tag)
		}
		digests[art.Digest] = true
	}

	for _, expectedDigest := range []string{"sha256:abc", "sha256:def", "sha256:ghi"} {
		if !digests[expectedDigest] {
			t.Errorf("expected digest %q to exist", expectedDigest)
		}
	}
}

func TestAddArtifact_GarbageCollectionBehavior(t *testing.T) {
	t.Run("moving tag prevents unbounded growth", func(t *testing.T) {
		idx := NewIndex()

		for i := 0; i < 100; i++ {
			idx.AddArtifact(ArtifactMetadata{
				Repository: "repo1",
				Tag:        "latest",
				Digest:     fmt.Sprintf("sha256:version-%d", i),
			})
		}

		arts := idx.GetArtifacts()
		if len(arts) != 1 {
			t.Fatalf("expected 1 artifact (GC removes old entries), got %d", len(arts))
		}
		if arts[0].Tag != "latest" || arts[0].Digest != "sha256:version-99" {
			t.Errorf("expected latest->sha256:version-99, got %s->%s", arts[0].Tag, arts[0].Digest)
		}
	})

	t.Run("multiple tags to same digest with GC", func(t *testing.T) {
		// Multiple tags pointing to same digest should coexist
		// Moving one tag should not affect others
		idx := NewIndex()

		// Create version 1.0.0 with multiple tags
		idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "v1.0.0", Digest: "sha256:release-1"})
		idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "stable", Digest: "sha256:release-1"})
		idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "latest", Digest: "sha256:release-1"})

		// Should have 3 entries, all pointing to same digest
		if len(idx.GetArtifacts()) != 3 {
			t.Fatalf("expected 3 tags for same digest, got %d", len(idx.GetArtifacts()))
		}

		// Release version 2.0.0 and move "latest" tag
		idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "v2.0.0", Digest: "sha256:release-2"})
		idx.AddArtifact(ArtifactMetadata{Repository: "repo1", Tag: "latest", Digest: "sha256:release-2"})

		// Should now have 4 entries: v1.0.0, stable (pointing to release-1), v2.0.0, latest (pointing to release-2)
		arts := idx.GetArtifacts()
		if len(arts) != 4 {
			t.Fatalf("expected 4 artifacts after moving latest tag, got %d", len(arts))
		}

		// Verify tags are correct
		tagDigests := make(map[string]string)
		for _, art := range arts {
			tagDigests[art.Tag] = art.Digest
		}

		if tagDigests["v1.0.0"] != "sha256:release-1" {
			t.Errorf("v1.0.0 should still point to release-1")
		}
		if tagDigests["stable"] != "sha256:release-1" {
			t.Errorf("stable should still point to release-1")
		}
		if tagDigests["v2.0.0"] != "sha256:release-2" {
			t.Errorf("v2.0.0 should point to release-2")
		}
		if tagDigests["latest"] != "sha256:release-2" {
			t.Errorf("latest should now point to release-2")
		}
	})

	t.Run("realistic release workflow with GC", func(t *testing.T) {
		// Simulate a realistic workflow:
		// 1. Push new version without tag (digest only)
		// 2. Tag it with semantic version
		// 3. Also tag as "latest"
		// 4. Repeat for multiple releases
		idx := NewIndex()

		for i := 1; i <= 5; i++ {
			digest := fmt.Sprintf("sha256:build-%d", i)
			version := fmt.Sprintf("v1.%d.0", i-1)

			// Push digest first (untagged)
			idx.AddArtifact(ArtifactMetadata{Repository: "myapp", Digest: digest})

			// Tag with version
			idx.AddArtifact(ArtifactMetadata{Repository: "myapp", Tag: version, Digest: digest})

			// Tag as latest (moves tag each iteration)
			idx.AddArtifact(ArtifactMetadata{Repository: "myapp", Tag: "latest", Digest: digest})
		}

		// Should have 6 entries: v1.0.0, v1.1.0, v1.2.0, v1.3.0, v1.4.0, latest
		// All semantic versions persist, "latest" was GC'd 4 times
		arts := idx.GetArtifacts()
		if len(arts) != 6 {
			t.Fatalf("expected 6 artifacts (5 versions + latest), got %d", len(arts))
		}

		// Verify latest points to most recent
		for _, art := range arts {
			if art.Tag == "latest" && art.Digest != "sha256:build-5" {
				t.Errorf("latest should point to build-5, got %s", art.Digest)
			}
		}
	})
}
