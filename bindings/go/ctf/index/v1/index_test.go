package v1

import (
	"bytes"
	"errors"
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
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}
	var foundOldTag bool
	for _, art := range arts {
		if art.Digest == "sha256:abc" && art.Tag != "" {
			foundOldTag = true
		}
	}
	if foundOldTag {
		t.Errorf("old artifact should have tag cleared after retag")
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
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}

	var latestCount int
	var oldArtifactHasTag bool
	var newArtifactHasLatestTag bool

	for _, art := range arts {
		if art.Tag == "latest" {
			latestCount++
			if art.Digest == "sha256:new" {
				newArtifactHasLatestTag = true
			}
		}
		if art.Digest == "sha256:old" && art.Tag != "" {
			oldArtifactHasTag = true
		}
	}

	if latestCount != 1 {
		t.Errorf("expected exactly 1 artifact with 'latest' tag, got %d", latestCount)
	}
	if !newArtifactHasLatestTag {
		t.Error("expected artifact with digest sha256:new to have 'latest' tag")
	}
	if oldArtifactHasTag {
		t.Error("expected old artifact (sha256:old) to have its tag cleared")
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
