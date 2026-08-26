package spec

import (
	"oras.land/oras-go/v2/content"
)

// Store defines the interface for interacting with an OCI store.
type Store interface {
	content.ReadOnlyGraphStorage
	content.Pusher
	content.TagResolver
}
