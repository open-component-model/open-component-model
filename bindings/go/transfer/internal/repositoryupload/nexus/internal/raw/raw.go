// Package raw uploads files into Nexus Repository 3 raw hosted repositories.
package raw

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/api"
)

// Store stores the content as is at a path with a plain PUT. A file already stored at the path is
// never overwritten, see [api.Repository.StoredFile].
type Store struct {
	repo         *api.Repository
	path, target string
	interval     time.Duration
}

var _ repositoryupload.Store = (*Store)(nil)

// New returns the store uploading to path (escaped, relative to the repository root) in repo.
func New(repo *api.Repository, path string, interval time.Duration) *Store {
	return &Store{repo: repo, path: path, target: repo.URL + "/" + path, interval: interval}
}

func (s *Store) Chart() bool { return false }

func (s *Store) URL() string { return s.target }

func (s *Store) Stored(ctx context.Context, known digest.Digest) (bool, error) {
	return s.repo.StoredFile(ctx, "raw", s.path, nil, known, s.interval)
}

func (s *Store) Put(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error) {
	computed, _, err := s.repo.Client.PutBlob(ctx, s.target, content, known, http.Header{"Content-Type": {mediaType}}, nil)
	return computed, err
}

func (s *Store) Discard(context.Context, digest.Digest) error {
	return fmt.Errorf("nexus keeps the uploaded file at %s", client.RedactURL(s.target))
}

func (s *Store) Publish(_ context.Context, _ digest.Digest, mediaType string) (runtime.Typed, error) {
	return repositoryupload.FileAccess(s.target, mediaType), nil
}
