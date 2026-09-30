// Package helm uploads charts into Artifactory Helm repositories.
package helm

import (
	"context"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/internal/api"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/chart"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
)

// Store deploys the chart to its file and publishes the chart name and version Artifactory
// records as properties when it indexes the chart.
type Store struct {
	*api.File
	helmURL string
}

var _ repositoryupload.Store = (*Store)(nil)

// New returns the store deploying the chart to file of the repository with Helm API helmURL.
func New(file *api.File, helmURL string) *Store {
	return &Store{File: file, helmURL: helmURL}
}

func (s *Store) Chart() bool { return true }

func (s *Store) Publish(ctx context.Context, _ digest.Digest, _ string) (runtime.Typed, error) {
	name, version, found, err := s.Package(ctx, "chart.name", "chart.version")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &repositoryupload.NotRecognizedError{Kind: "a helm chart", Reason: "artifactory recorded no chart name and version for " + client.RedactURL(s.URL())}
	}
	return chart.Access("artifactory", s.helmURL, name, version, client.RedactURL(s.URL()))
}
