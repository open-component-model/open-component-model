// Package fetch provides core functionality for retrieving OCI artifacts and their contents.
// It handles the low-level operations of fetching manifests, layers, and blobs from OCI registries.
package fetch

import (
	"ocm.software/open-component-model/bindings/go/blob"
)

// LocalBlob represents a content-addressable piece of data stored in an OCI repository.
// It provides a unified interface for accessing both the content and metadata of OCI blobs.
type LocalBlob interface {
	blob.ReadOnlyBlob
	blob.SizeAware
	blob.DigestAware
	blob.MediaTypeAware
}
