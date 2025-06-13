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
	// If an artifact with the same digest already exists, its tag is updated (if provided).
	// If a tag already exists in the same repository but points to a different digest, it is cleared ("retag" scenario).
	// If no artifact with the same digest exists, the artifact is added to the index.
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
// A common mapping is to have an artifact metadata mapping to an OCI Image Layout with its own index containing
// exactly one tag.
// In the future it might become common to have multiple tags per artifact, but this is not expected in most cases.
type ArtifactMetadata struct {
	// The Repository Name of the artifact. Relative Name of the artifact, no FQDN
	Repository string `json:"repository"`
	// The Tag of the artifact. This is the tag that is used to reference the artifact.
	// Only relevant if artifact contains exactly one version.
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

	newArtifact := true
	// If we have a tag, lets compare it with existing artifacts in our index.
	for idx, art := range i.Artifacts {
		if art.Repository != a.Repository {
			continue
		}
		if art.Tag == a.Tag && art.Digest != a.Digest {
			// "retag" scenario to new digest: tag exists with different digest → clear old tag
			i.Artifacts[idx].Tag = ""
		}
		if art.Digest == a.Digest {
			//  "same digest" scenario: artifact already exists with same digest.
			newArtifact = false
			if a.Tag != "" {
				// "tag" scenario: digest is equivalent but there is now a tag.
				i.Artifacts[idx].Tag = a.Tag
			}
		}
	}

	if newArtifact {
		// Add new artifact
		i.Artifacts = append(i.Artifacts, a)
	}
}

func (i *index) GetArtifacts() []ArtifactMetadata {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return slices.Clone(i.Artifacts)
}
