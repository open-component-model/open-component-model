package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"ocm.software/open-component-model/bindings/go/blob/compression"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// nexusChartMetadataAttempts bounds how often Nexus's component search is polled for an uploaded
// chart. Nexus indexes uploaded components for search asynchronously (about 2 s on Nexus 3.96).
const nexusChartMetadataAttempts = 30

// helmRepositoryServer is the server-specific part of a HelmRepositoryUpload.
type helmRepositoryServer interface {
	// name is "artifactory" or "nexus", used in logs and errors.
	name() string
	// helmRepository is the published Helm/v1 helmRepository.
	helmRepository() string
	// uploadURL is where the chart is PUT.
	uploadURL() string
	// uploadHeader returns the upload request header; sha256Hex is empty when unknown.
	uploadHeader(sha256Hex string) http.Header
	// reuse makes content the repository already stores under sha256Hex available without uploading it.
	reuse(ctx context.Context, c *helmClient, sha256Hex string) (bool, error)
	// rejectedUploadStored reports whether the repository stores the content of an upload it rejected.
	rejectedUploadStored(ctx context.Context, c *helmClient, sha256Hex string) bool
	// chart returns the chart name and version the server recorded; found=false when none.
	chart(ctx context.Context, c *helmClient, sha256Hex string) (name, version string, found bool, err error)
	// discard removes uploaded content that must not be published.
	discard(ctx context.Context, c *helmClient, sha256Hex string) error
	// afterUpload runs follow-up requests; failures are logged, never returned.
	afterUpload(ctx context.Context, c *helmClient)
}

// newServer returns the backend of spec.Server. path is the chart location below the component
// version, file the chart file name, both path-escaped.
func (t *HelmRepositoryUpload) newServer(spec *HelmRepositoryUploadSpec, path, file string) (helmRepositoryServer, error) {
	interval := t.chartMetadataInterval
	if interval == 0 {
		interval = defaultChartMetadataInterval
	}
	switch spec.Server {
	case transferv1alpha1.HelmRepositoryServerArtifactory:
		return newArtifactoryServer(spec, path, interval)
	case transferv1alpha1.HelmRepositoryServerNexus:
		return newNexusServer(spec, file, interval)
	default:
		return nil, fmt.Errorf("unsupported helm repository server %q", spec.Server)
	}
}

// artifactoryServer deploys the chart to
// <url>/artifactory/<repository>/<component>/<component version>/<chart file> and reads the chart
// name and version from the properties Artifactory records when it indexes the deployed chart.
type artifactoryServer struct {
	putURL, storageURL, helmRepo, reindexURL string
	reindex                                  bool
	interval                                 time.Duration
}

func newArtifactoryServer(spec *HelmRepositoryUploadSpec, path string, interval time.Duration) (*artifactoryServer, error) {
	uploadBase, err := url.JoinPath(spec.URL, "artifactory", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	storageBase, err := url.JoinPath(spec.URL, "artifactory", "api", "storage", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	helmRepo, err := url.JoinPath(spec.URL, "artifactory", "api", "helm", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	return &artifactoryServer{
		putURL:     uploadBase + "/" + path,
		storageURL: storageBase + "/" + path,
		helmRepo:   helmRepo,
		reindexURL: helmRepo + "/" + path + "/reindex",
		reindex:    spec.Reindex,
		interval:   interval,
	}, nil
}

func (a *artifactoryServer) name() string           { return "artifactory" }
func (a *artifactoryServer) helmRepository() string { return a.helmRepo }
func (a *artifactoryServer) uploadURL() string      { return a.putURL }

func (a *artifactoryServer) uploadHeader(sha256Hex string) http.Header {
	header := http.Header{"Content-Type": {compression.MediaTypeGzip}}
	if sha256Hex != "" {
		// Artifactory verifies the uploaded bytes against this checksum and rejects the upload
		// on mismatch, so a corrupted stream is never stored.
		header.Set("X-Checksum-Sha256", sha256Hex)
	}
	return header
}

// reuse asks Artifactory to deploy the upload URL from content it already stores under the
// checksum ("Deploy Artifact by Checksum"), so the chart is not uploaded again. It reports false
// when Artifactory does not have the content (404) or declines the request otherwise; the caller
// then uploads the chart, which surfaces real errors such as missing permissions.
func (a *artifactoryServer) reuse(ctx context.Context, c *helmClient, sha256Hex string) (bool, error) {
	resp, err := c.do(ctx, http.MethodPut, a.putURL, nil, 0, http.Header{
		"X-Checksum-Deploy": {"true"},
		"X-Checksum-Sha256": {sha256Hex},
	})
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// rejectedUploadStored is false: a rejected Artifactory upload fails the transfer.
func (a *artifactoryServer) rejectedUploadStored(context.Context, *helmClient, string) bool {
	return false
}

// chart reads the chart name and version Artifactory records as properties of the deployed chart.
// It polls briefly in case the metadata is calculated asynchronously and reports found=false when
// Artifactory recorded none, i.e. the content is not a helm chart.
func (a *artifactoryServer) chart(ctx context.Context, c *helmClient, _ string) (string, string, bool, error) {
	target := a.storageURL + "?properties=chart.name,chart.version"
	for attempt := 1; ; attempt++ {
		resp, err := c.do(ctx, http.MethodGet, target, nil, -1, nil)
		if err != nil {
			return "", "", false, err
		}
		var props struct {
			Properties map[string][]string `json:"properties"`
		}
		switch resp.StatusCode {
		case http.StatusOK:
			err = json.NewDecoder(io.LimitReader(resp.Body, maxErrorBodyBytes)).Decode(&props)
			_ = resp.Body.Close()
			if err != nil {
				return "", "", false, fmt.Errorf("failed decoding chart properties of %s: %w", redactURL(a.storageURL), err)
			}
			if names, versions := props.Properties["chart.name"], props.Properties["chart.version"]; len(names) == 1 && len(versions) == 1 {
				return names[0], versions[0], names[0] != "" && versions[0] != "", nil
			}
		case http.StatusNotFound:
			_ = resp.Body.Close()
		default:
			_ = resp.Body.Close()
			return "", "", false, fmt.Errorf("GET %s returned status %d", redactURL(target), resp.StatusCode)
		}
		if attempt == chartMetadataAttempts {
			return "", "", false, nil
		}
		select {
		case <-ctx.Done():
			return "", "", false, ctx.Err()
		case <-time.After(a.interval):
		}
	}
}

func (a *artifactoryServer) discard(ctx context.Context, c *helmClient, _ string) error {
	return c.send(ctx, http.MethodDelete, a.putURL, nil, -1, nil)
}

func (a *artifactoryServer) afterUpload(ctx context.Context, c *helmClient) {
	if !a.reindex {
		return
	}
	if err := c.send(ctx, http.MethodPost, a.reindexURL, nil, -1, nil); err != nil {
		slog.WarnContext(ctx, "failed requesting a helm index recalculation for the uploaded chart; artifactory also indexes deployed charts on its own",
			"url", redactURL(a.putURL), "error", err)
	}
}

// nexusServer uploads the chart to the root of a Nexus Repository 3 Helm hosted repository.
// Nexus stores it under the path it derives from Chart.yaml (<name>-<version>.tgz), ignoring the
// uploaded file name, and the chart name and version are read from Nexus's component search by
// SHA-256.
type nexusServer struct {
	repository, helmRepo, putURL, searchURL string
	interval                                time.Duration
}

// nexusComponent is a component item of Nexus's search API.
type nexusComponent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Format  string `json:"format"`
}

func newNexusServer(spec *HelmRepositoryUploadSpec, file string, interval time.Duration) (*nexusServer, error) {
	helmRepo, err := url.JoinPath(spec.URL, "repository", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	searchURL, err := url.JoinPath(spec.URL, "service", "rest", "v1", "search")
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	return &nexusServer{
		repository: spec.Repository,
		helmRepo:   helmRepo,
		putURL:     helmRepo + "/" + file,
		searchURL:  searchURL,
		interval:   interval,
	}, nil
}

func (n *nexusServer) name() string           { return "nexus" }
func (n *nexusServer) helmRepository() string { return n.helmRepo }
func (n *nexusServer) uploadURL() string      { return n.putURL }

// uploadHeader carries no checksum: Nexus does not verify one, the caller compares after upload.
func (n *nexusServer) uploadHeader(string) http.Header {
	return http.Header{"Content-Type": {compression.MediaTypeGzip}}
}

// reuse reports whether the repository already stores a chart with the content, which Nexus then
// publishes under its own path, so nothing needs to be uploaded.
func (n *nexusServer) reuse(ctx context.Context, c *helmClient, sha256Hex string) (bool, error) {
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

// rejectedUploadStored reports whether the repository stores a chart with the content of an upload
// it rejected, as Nexus does when redeploy is disabled and the chart was uploaded before. It polls
// like chart, because the earlier upload may not be searchable yet.
func (n *nexusServer) rejectedUploadStored(ctx context.Context, c *helmClient, sha256Hex string) bool {
	_, _, found, err := n.chart(ctx, c, sha256Hex)
	return err == nil && found
}

// chart polls the component search until it finds the content and fails when the content is
// stored as more than one chart name and version.
func (n *nexusServer) chart(ctx context.Context, c *helmClient, sha256Hex string) (string, string, bool, error) {
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
		if attempt == nexusChartMetadataAttempts {
			return "", "", false, nil
		}
		select {
		case <-ctx.Done():
			return "", "", false, ctx.Err()
		case <-time.After(n.interval):
		}
	}
}

// discard deletes nothing: Nexus chooses the path from the chart, and the content may predate
// this upload.
func (n *nexusServer) discard(_ context.Context, _ *helmClient, sha256Hex string) error {
	return fmt.Errorf("nexus stores charts under a path derived from the chart, so content with SHA-256 %s is left in repository %s", sha256Hex, n.repository)
}

func (n *nexusServer) afterUpload(context.Context, *helmClient) {}

// search returns the helm components of the repository whose asset has the SHA-256. Only the
// first page is read: one content is expected to be stored as at most one component.
func (n *nexusServer) search(ctx context.Context, c *helmClient, sha256Hex string) ([]nexusComponent, error) {
	target := n.searchURL + "?" + url.Values{
		"repository": {n.repository},
		"format":     {"helm"},
		"sha256":     {sha256Hex},
	}.Encode()
	resp, err := c.do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", redactURL(target), resp.StatusCode)
	}
	var page struct {
		Items []nexusComponent `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&page); err != nil {
		return nil, fmt.Errorf("failed decoding nexus search result of %s: %w", redactURL(target), err)
	}
	return page.Items, nil
}
