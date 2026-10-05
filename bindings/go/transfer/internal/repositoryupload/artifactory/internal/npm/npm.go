// Package npm uploads package tarballs into Artifactory npm repositories.
package npm

import (
	"context"
	"log/slog"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/internal/api"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
)

// Store deploys the tarball to its file and publishes it once Artifactory recorded the package
// name and version from its package.json, which makes it installable.
type Store struct {
	*api.File
}

var _ repositoryupload.Store = (*Store)(nil)

// New returns the store deploying the tarball to file.
func New(file *api.File) *Store {
	return &Store{File: file}
}

func (s *Store) Chart() bool { return false }

func (s *Store) Publish(ctx context.Context, _ digest.Digest, mediaType string) (runtime.Typed, error) {
	name, version, found, err := s.Package(ctx, "npm.name", "npm.version")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &repositoryupload.NotRecognizedError{Kind: "an npm package", Reason: "artifactory recorded no npm.name and npm.version for " + client.RedactURL(s.URL())}
	}
	slog.InfoContext(ctx, "artifactory indexed the npm package", "url", client.RedactURL(s.URL()), "package", name+"@"+version)
	return repositoryupload.FileAccess(s.URL(), mediaType), nil
}
