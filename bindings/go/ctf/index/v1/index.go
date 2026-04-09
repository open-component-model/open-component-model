package v1

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
)

const (
	SchemaVersion         = 1
	ArtifactIndexFileName = "artifact-index.json"
)

var ErrSchemaVersionMismatch = fmt.Errorf("schema version mismatch, only %v is supported", SchemaVersion)

// Index is a collection of artifacts that can be serialized to disk.
// It is used to store metadata about the artifacts in a CTF and used for discovery purposes
// The Index is canonically stored in the root of a CTF as ArtifactIndexFileName with SchemaVersion.
type Index interface {
	// AddArtifact adds an ArtifactMetadata to the index.
	//
	// Like OCI Image Layout, multiple entries with the same digest but different tags are allowed.
	// If a tag already exists in the same repository but points to a different digest, the old entry is removed.
	// If an exact duplicate (same repository, tag, and digest) exists, the add is skipped.
	AddArtifact(a ArtifactMetadata)
	// GetArtifacts returns a slice of ArtifactMetadata that are stored in the index at the time of the call.
	// It is not guaranteed to be consistent with later calls as it is a snapshot of the current state.
	GetArtifacts() []ArtifactMetadata
}

type index struct {
	mu        sync.RWMutex
	Versioned `json:",inline"`
	Artifacts []ArtifactMetadata `json:"artifacts"`
}

// ArtifactMetadata is a struct that contains metadata about an artifact stored in a CTF.
// Since CTFs are registry-like, the metadata is similar to that of a container repository.
// Each entry points to an OCI manifest or index blob by digest, with blobs stored flat in the CTF.
// Like OCI Image Layout, multiple entries with the same digest but different tags can coexist,
// allowing multiple tags (e.g., "v1.0.0", "latest") to point to the same artifact.
type ArtifactMetadata struct {
	// The Repository Name of the artifact. Relative Name of the artifact, no FQDN
	Repository string `json:"repository"`
	// The Tag of the artifact. This is the tag that is used to reference the artifact.
	Tag string `json:"tag,omitempty"`
	// The Digest of the artifact. This is the digest that is used to reference the artifact.
	// Points to the blob in the CTF that contains the artifact.
	Digest string `json:"digest,omitempty"`
	// MediaType is the media type of the artifact. This is the media type that is used to reference the artifact.
	// The MediaType is optionally added and can be left empty. In this case it is assumed that the artifact
	// is of an arbitrary type.
	MediaType string `json:"mediaType,omitempty"`
}

// DecodeIndex reads an Index from the provided reader.
func DecodeIndex(data io.Reader) (Index, error) {
	var d index

	decoder := json.NewDecoder(data)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&d); err != nil {
		return nil, err
	}

	if d.SchemaVersion != SchemaVersion {
		return nil, ErrSchemaVersionMismatch
	}

	return &d, nil
}

// Encode serializes the Index to a byte slice.
func Encode(d Index) ([]byte, error) {
	return json.Marshal(d)
}

// NewIndex creates a new Index instance defaulted to SchemaVersion.
func NewIndex() Index {
	return &index{
		Versioned: Versioned{
			SchemaVersion: SchemaVersion,
		},
	}
}

func (i *index) AddArtifact(a ArtifactMetadata) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// skip exact duplicate entries
	for _, art := range i.Artifacts {
		if art.Repository == a.Repository && art.Tag == a.Tag && art.Digest == a.Digest {
			return
		}
	}

	if a.Tag != "" {
		// If adding a tagged artifact, remove any existing entry with same repo+tag but different digest
		// This handles the "retag" scenario: moving a tag from one digest to another
		for idx, art := range i.Artifacts {
			if art.Repository == a.Repository && art.Tag == a.Tag {
				i.Artifacts = slices.Delete(i.Artifacts, idx, idx+1)
				break
			}
		}

		// Check if we should tag an existing untagged entry instead of adding new
		// This handles the pattern: artifact added without tag, then tagged later
		for idx, art := range i.Artifacts {
			if art.Repository == a.Repository && art.Tag == "" && art.Digest == a.Digest {
				i.Artifacts[idx].Tag = a.Tag
				return
			}
		}
	}

	// No matching artifact found, add as new entry
	i.Artifacts = append(i.Artifacts, a)
}

func (i *index) GetArtifacts() []ArtifactMetadata {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return slices.Clone(i.Artifacts)
}
