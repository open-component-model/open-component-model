package nexus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
)

// helmServer uploads the chart to the root of a Nexus Repository 3 Helm hosted repository.
// Nexus stores it under the path it derives from Chart.yaml (<name>-<version>.tgz), ignoring the
// uploaded file name, and the chart name and version are read from Nexus's component search by
// SHA-256.
type helmServer struct {
	repository, helmRepo, putURL, searchURL string
	interval                                time.Duration
}

// component is a component item of Nexus's search API.
type component struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Format  string `json:"format"`
}

func newHelmServer(spec *repositoryupload.Spec, file string, interval time.Duration) (*helmServer, error) {
	helmRepo, err := url.JoinPath(spec.URL, "repository", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	searchURL, err := url.JoinPath(spec.URL, "service", "rest", "v1", "search")
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	return &helmServer{
		repository: spec.Repository,
		helmRepo:   helmRepo,
		putURL:     helmRepo + "/" + file,
		searchURL:  searchURL,
		interval:   interval,
	}, nil
}

func (n *helmServer) Name() string           { return "nexus" }
func (n *helmServer) HelmRepository() string { return n.helmRepo }
func (n *helmServer) UploadURL() string      { return n.putURL }
func (n *helmServer) DeployURL() string      { return n.putURL }

// Claim leaves the location to Nexus: it stores the chart under a path derived from the chart,
// and its redeploy policy decides whether a stored chart may be replaced.
func (n *helmServer) Claim(context.Context, *repositoryupload.Client, string) (bool, error) {
	return false, nil
}

// UploadHeader carries no checksum: Nexus does not verify one, the caller compares after upload.
func (n *helmServer) UploadHeader(string) http.Header {
	return http.Header{"Content-Type": {compression.MediaTypeGzip}}
}

// Reuse reports whether the repository already stores a chart with the content, which Nexus then
// publishes under its own path, so nothing needs to be uploaded.
func (n *helmServer) Reuse(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	items, err := n.search(ctx, c, sha256Hex)
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

// RejectedUploadStored reports whether the repository stores a chart with the content of an upload
// it rejected, as Nexus does when redeploy is disabled and the chart was uploaded before. It polls
// like Chart, because the earlier upload may not be searchable yet.
func (n *helmServer) RejectedUploadStored(ctx context.Context, c *repositoryupload.Client, sha256Hex string) bool {
	_, _, found, err := n.Chart(ctx, c, sha256Hex)
	return err == nil && found
}

// Chart polls the component search until it finds the content and fails when the content is
// stored as more than one chart name and version.
func (n *helmServer) Chart(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (string, string, bool, error) {
	for attempt := 1; ; attempt++ {
		items, err := n.search(ctx, c, sha256Hex)
		if err != nil {
			return "", "", false, err
		}
		charts := map[[2]string]struct{}{}
		for _, item := range items {
			if item.Name != "" && item.Version != "" {
				charts[[2]string{item.Name, item.Version}] = struct{}{}
			}
		}
		if len(charts) > 1 {
			return "", "", false, fmt.Errorf("nexus stores content %s as more than one chart", sha256Hex)
		}
		for chart := range charts {
			return chart[0], chart[1], true, nil
		}
		if attempt == repositoryupload.PollAttempts {
			return "", "", false, nil
		}
		select {
		case <-ctx.Done():
			return "", "", false, ctx.Err()
		case <-time.After(n.interval):
		}
	}
}

// Discard deletes nothing: Nexus chooses the path from the chart, and the content may predate
// this upload.
func (n *helmServer) Discard(_ context.Context, _ *repositoryupload.Client, sha256Hex string) error {
	return fmt.Errorf("nexus stores charts under a path derived from the chart, so content with SHA-256 %s is left in repository %s", sha256Hex, n.repository)
}

// search returns the helm components of the repository whose asset has the SHA-256. Only the
// first page is read: one content is expected to be stored as at most one component.
func (n *helmServer) search(ctx context.Context, c *repositoryupload.Client, sha256Hex string) ([]component, error) {
	target := n.searchURL + "?" + url.Values{
		"repository": {n.repository},
		"format":     {"helm"},
		"sha256":     {sha256Hex},
	}.Encode()
	resp, err := c.Do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", repositoryupload.RedactURL(target), resp.StatusCode)
	}
	var page struct {
		Items []component `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&page); err != nil {
		return nil, fmt.Errorf("failed decoding nexus search result of %s: %w", repositoryupload.RedactURL(target), err)
	}
	return page.Items, nil
}
