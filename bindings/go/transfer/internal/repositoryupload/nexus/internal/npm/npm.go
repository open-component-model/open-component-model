// Package npm uploads package tarballs into Nexus Repository 3 npm hosted repositories.
package npm

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/api"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// Store uploads the package tarball through the components API. Nexus reads the package name
// and version from package.json and stores the tarball under <name>/-/<name>-<version>.tgz, so
// the tarball is found by its digest afterwards. A tarball the repository already stores is
// reused without uploading it.
type Store struct {
	repo     *api.Repository
	file     string
	interval time.Duration
	// path is the escaped path, with a leading slash, of the stored tarball once found.
	path string
}

var _ repositoryupload.Store = (*Store)(nil)

// New returns the store uploading the tarball as file into repo.
func New(repo *api.Repository, file string, interval time.Duration) *Store {
	return &Store{repo: repo, file: file, interval: interval}
}

func (s *Store) Chart() bool { return false }

func (s *Store) URL() string { return s.repo.URL + s.path }

func (s *Store) Stored(ctx context.Context, known digest.Digest) (bool, error) {
	if _, ok := api.ChecksumQuery(known); !ok {
		return false, nil
	}
	return s.find(ctx, known)
}

func (s *Store) Put(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error) {
	return s.repo.UploadComponent(ctx, nil, "npm.asset", s.file, content, known, mediaType)
}

func (s *Store) Discard(context.Context, digest.Digest) error {
	return fmt.Errorf("nexus keeps the uploaded package in repository %s", s.repo.Name)
}

// Publish returns a Wget/v1 access on the stored tarball. Nexus indexes it for search shortly
// after the upload, so it is polled for.
func (s *Store) Publish(ctx context.Context, stored digest.Digest, mediaType string) (runtime.Typed, error) {
	if s.path == "" {
		found, err := repositoryupload.Poll(ctx, s.interval, func() (bool, error) { return s.find(ctx, stored) })
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("nexus repository %q stored the npm package %s, but its search does not find it", s.repo.Name, stored)
		}
	}
	return &wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: s.repo.URL + s.path, MediaType: mediaType}, nil
}

// find looks up the first .tgz asset the repository stores with content d and remembers its path.
func (s *Store) find(ctx context.Context, d digest.Digest) (bool, error) {
	query, ok := api.ChecksumQuery(d)
	if !ok {
		return false, fmt.Errorf("nexus cannot search for content %s", d)
	}
	assets, err := s.repo.SearchAssets(ctx, query)
	if err != nil {
		return false, err
	}
	for _, a := range assets {
		if !strings.HasSuffix(a.Path, ".tgz") {
			continue
		}
		segments := strings.Split(strings.TrimPrefix(a.Path, "/"), "/")
		for i, segment := range segments {
			segments[i] = url.PathEscape(segment)
		}
		s.path = "/" + strings.Join(segments, "/")
		return true, nil
	}
	return false, nil
}
