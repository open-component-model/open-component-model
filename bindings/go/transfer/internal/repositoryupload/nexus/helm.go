package nexus

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// helmStore uploads the chart to the root of a Nexus Repository 3 Helm hosted repository.
// Nexus stores it under the path it derives from Chart.yaml (<name>-<version>.tgz), ignoring the
// uploaded file name, and the chart name and version are read from Nexus's component search by
// SHA-256. Its redeploy policy decides whether a stored chart may be replaced.
type helmStore struct {
	repository, helmRepo, putURL, searchURL string
	interval                                time.Duration
}

// component is a component item of Nexus's search API.
type component struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func newHelmStore(spec *uploadv1alpha1.RepositoryUploadSpec, helmRepo, file string, interval time.Duration) (*helmStore, error) {
	searchURL, err := url.JoinPath(spec.URL, "service", "rest", "v1", "search")
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	return &helmStore{
		repository: spec.Repository,
		helmRepo:   helmRepo,
		putURL:     helmRepo + "/" + file,
		searchURL:  searchURL,
		interval:   interval,
	}, nil
}

func (n *helmStore) Chart() bool { return true }

func (n *helmStore) URL() string { return n.putURL }

// Stored reports whether the repository already stores a chart with the content, which Nexus
// publishes under its own path, so nothing needs to be uploaded.
func (n *helmStore) Stored(ctx context.Context, c *repositoryupload.Client, known digest.Digest) (bool, error) {
	if _, ok := checksumQuery(known); !ok {
		return false, nil
	}
	items, err := n.search(ctx, c, known)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.Name != "" && item.Version != "" {
			return true, nil
		}
	}
	return false, nil
}

// Put uploads the chart without a checksum: Nexus does not verify one. A repository that
// rejects redeploying a chart still stores its content, e.g. from an earlier transfer of the same
// resource without a source digest, so a rejected upload of content whose digest was unknown up
// front succeeds when the repository stores it.
func (n *helmStore) Put(ctx context.Context, c *repositoryupload.Client, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error) {
	computed, complete, err := repositoryupload.UploadBlob(ctx, c, content, known, n.putURL, http.Header{"Content-Type": {mediaType}}, nil)
	if err == nil || known != "" || !complete {
		return computed, err
	}
	if _, _, found, chartErr := n.chart(ctx, c, computed); chartErr != nil || !found {
		return computed, err
	}
	slog.InfoContext(ctx, "helm repository already stores the chart the upload was rejected for", "server", "nexus", "url", repositoryupload.RedactURL(n.putURL))
	return computed, nil
}

// Discard deletes nothing: Nexus chooses the path from the chart, and the content may predate
// this upload.
func (n *helmStore) Discard(_ context.Context, _ *repositoryupload.Client, uploaded digest.Digest) error {
	return fmt.Errorf("nexus stores charts under a path derived from the chart, so content %s is left in repository %s", uploaded, n.repository)
}

func (n *helmStore) Publish(ctx context.Context, c *repositoryupload.Client, stored digest.Digest, _ string) (runtime.Typed, error) {
	name, version, found, err := n.chart(ctx, c, stored)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &repositoryupload.NotRecognizedError{Kind: "a helm chart", Reason: "nexus recorded no chart name and version for " + repositoryupload.RedactURL(n.putURL)}
	}
	return repositoryupload.HelmAccess("nexus", n.helmRepo, name, version, repositoryupload.RedactURL(n.putURL))
}

// chart polls the component search until it finds the content and fails when the content is
// stored as more than one chart name and version.
func (n *helmStore) chart(ctx context.Context, c *repositoryupload.Client, d digest.Digest) (name, version string, found bool, err error) {
	found, err = repositoryupload.Poll(ctx, n.interval, func() (bool, error) {
		items, err := n.search(ctx, c, d)
		if err != nil {
			return false, err
		}
		charts := map[component]struct{}{}
		for _, item := range items {
			if item.Name != "" && item.Version != "" {
				charts[item] = struct{}{}
			}
		}
		if len(charts) > 1 {
			return false, fmt.Errorf("nexus stores content %s as more than one chart", d)
		}
		for chart := range charts {
			name, version = chart.Name, chart.Version
			return true, nil
		}
		return false, nil
	})
	return name, version, found, err
}

// search returns the helm components of the repository whose asset has digest d. Only the
// first page is read: one content is expected to be stored as at most one component.
func (n *helmStore) search(ctx context.Context, c *repositoryupload.Client, d digest.Digest) ([]component, error) {
	query, ok := checksumQuery(d)
	if !ok {
		return nil, fmt.Errorf("nexus cannot search for content %s", d)
	}
	query.Set("repository", n.repository)
	query.Set("format", "helm")
	target := n.searchURL + "?" + query.Encode()
	var page struct {
		Items []component `json:"items"`
	}
	err := c.Send(ctx, http.MethodGet, target, nil, -1, nil, &page)
	return page.Items, err
}
