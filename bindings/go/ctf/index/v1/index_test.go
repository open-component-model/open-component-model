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
