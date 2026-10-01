// Package helm uploads charts into Nexus Repository 3 Helm hosted repositories.
package helm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/chart"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/api"
)

// Store uploads the chart to the root of the repository. Nexus stores it under the path it
// derives from Chart.yaml (<name>-<version>.tgz), ignoring the uploaded file name, and the chart
// name and version are read from the component search by digest. The redeploy policy of the
// repository decides whether a stored chart may be replaced.
type Store struct {
	repo     *api.Repository
	putURL   string
	interval time.Duration
}

var _ repositoryupload.Store = (*Store)(nil)

// New returns the store uploading the chart as file into repo.
func New(repo *api.Repository, file string, interval time.Duration) *Store {
	return &Store{repo: repo, putURL: repo.URL + "/" + file, interval: interval}
}

func (s *Store) Chart() bool { return true }

func (s *Store) URL() string { return s.putURL }

// Stored reports whether the repository already stores a chart with the content, which Nexus
// publishes under its own path, so nothing needs to be uploaded.
func (s *Store) Stored(ctx context.Context, known digest.Digest) (bool, error) {
	if _, ok := api.ChecksumQuery(known); !ok {
		return false, nil
	}
	components, err := s.search(ctx, known)
	if err != nil {
		return false, err
	}
	for _, c := range components {
		if c.Name != "" && c.Version != "" {
			return true, nil
		}
	}
	return false, nil
}

// Put uploads the chart without a checksum: Nexus does not verify one. A repository that
// rejects redeploying a chart still stores its content, e.g. from an earlier transfer of the same
// resource without a source digest, so a rejected upload of content whose digest was unknown up
// front succeeds when the repository stores it.
func (s *Store) Put(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error) {
	computed, complete, err := s.repo.Client.PutBlob(ctx, s.putURL, content, known, http.Header{"Content-Type": {mediaType}}, nil)
	if err == nil || known != "" || !complete {
		return computed, err
	}
	if _, _, found, chartErr := s.chart(ctx, computed); chartErr != nil || !found {
		return computed, err
	}
	slog.InfoContext(ctx, "helm repository already stores the chart the upload was rejected for", "server", "nexus", "url", client.RedactURL(s.putURL))
	return computed, nil
}

// Discard deletes nothing: Nexus chooses the path from the chart, and the content may predate
// this upload.
func (s *Store) Discard(_ context.Context, uploaded digest.Digest) error {
	return fmt.Errorf("nexus stores charts under a path derived from the chart, so content %s is left in repository %s", uploaded, s.repo.Name)
}

func (s *Store) Publish(ctx context.Context, stored digest.Digest, _ string) (runtime.Typed, error) {
	name, version, found, err := s.chart(ctx, stored)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &repositoryupload.NotRecognizedError{Kind: "a helm chart", Reason: "nexus recorded no chart name and version for " + client.RedactURL(s.putURL)}
	}
	return chart.Access("nexus", s.repo.URL, name, version, client.RedactURL(s.putURL))
}

// chart polls the component search until it finds the content and fails when the content is
// stored as more than one chart name and version.
func (s *Store) chart(ctx context.Context, d digest.Digest) (name, version string, found bool, err error) {
	found, err = repositoryupload.Poll(ctx, s.interval, func() (bool, error) {
		components, err := s.search(ctx, d)
		if err != nil {
			return false, err
		}
		charts := map[api.Component]struct{}{}
		for _, c := range components {
			if c.Name != "" && c.Version != "" {
				charts[c] = struct{}{}
			}
		}
		if len(charts) > 1 {
			return false, fmt.Errorf("nexus stores content %s as more than one chart", d)
		}
		for c := range charts {
			name, version = c.Name, c.Version
			return true, nil
		}
		return false, nil
	})
	return name, version, found, err
}

// search returns the helm components of the repository whose asset has content d. One content
// is expected to be stored as at most one component, so only the first page is read.
func (s *Store) search(ctx context.Context, d digest.Digest) ([]api.Component, error) {
	query, ok := api.ChecksumQuery(d)
	if !ok {
		return nil, fmt.Errorf("nexus cannot search for content %s", d)
	}
	query.Set("format", "helm")
	return s.repo.SearchComponents(ctx, query)
}
