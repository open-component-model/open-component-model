package spec

import (
	"github.com/oras-project/oras-go/v3/content"
)

// Store defines the interface for interacting with an OCI store.
type Store interface {
	content.ReadOnlyGraphStorage
	content.Pusher
	content.TagResolver
}
