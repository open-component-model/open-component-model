// Package generic uploads files into Artifactory generic and maven repositories.
package generic

import (
	"context"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/internal/api"
)

// Store deploys the content as is to its file and publishes the stored file.
type Store struct {
	*api.File
}

var _ repositoryupload.Store = (*Store)(nil)

// New returns the store deploying the content to file.
func New(file *api.File) *Store {
	return &Store{File: file}
}

func (s *Store) Chart() bool { return false }

func (s *Store) Publish(_ context.Context, _ digest.Digest, mediaType string) (runtime.Typed, error) {
	return repositoryupload.FileAccess(s.URL(), mediaType), nil
}
