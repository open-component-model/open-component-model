package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/blob/compression"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// helmRepositoryServer is the server-specific part of a HelmRepositoryUpload.
type helmRepositoryServer interface {
	// name is "artifactory" or "nexus", used in logs and errors.
	name() string
	// helmRepository is the published Helm/v1 helmRepository.
	helmRepository() string
	// uploadURL is where the chart is stored; it identifies the upload in logs and credentials.
	uploadURL() string
	// deployURL is where the chart is PUT: uploadURL plus server-specific request parameters.
	deployURL() string
	// uploadHeader returns the upload request header; sha256Hex is empty when unknown.
	uploadHeader(sha256Hex string) http.Header
	// claim checks that the upload location may be written for this resource. stored reports
	// that it already holds content with sha256Hex, so nothing needs to be written.
	claim(ctx context.Context, c *helmClient, sha256Hex string) (stored bool, err error)
	// reuse makes content the repository already stores under sha256Hex available without uploading it.
	reuse(ctx context.Context, c *helmClient, sha256Hex string) (bool, error)
	// rejectedUploadStored reports whether the repository stores the content of an upload it rejected.
	rejectedUploadStored(ctx context.Context, c *helmClient, sha256Hex string) bool
	// chart returns the chart name and version the server recorded for the stored content;
	// isChart=false when it recorded none, i.e. did not recognize the content as a chart.
	chart(ctx context.Context, c *helmClient, sha256Hex string) (name, version string, isChart bool, err error)
	// discard removes uploaded content that must not be published.
	discard(ctx context.Context, c *helmClient, sha256Hex string) error
}

// chartProperty is an Artifactory property recorded on a deployed chart.
type chartProperty struct {
	key, value string
}

// newServer returns the backend of spec.Server. path is the chart location in the repository,
// file the chart file name, both path-escaped. owner identifies the resource the chart is
// uploaded for.
func (t *HelmRepositoryUpload) newServer(spec *HelmRepositoryUploadSpec, path, file string, owner []chartProperty) (helmRepositoryServer, error) {
	interval := t.chartMetadataInterval
	if interval == 0 {
		interval = defaultChartMetadataInterval
	}
	switch spec.Server {
	case transferv1alpha1.HelmRepositoryServerArtifactory:
		return newArtifactoryServer(spec, path, owner, interval)
	case transferv1alpha1.HelmRepositoryServerNexus:
		return newNexusServer(spec, file, interval)
	default:
		return nil, fmt.Errorf("unsupported helm repository server %q", spec.Server)
	}
}

// artifactoryServer deploys the chart to <url>/artifactory/<repository>/<path> with the owner
// properties and reads the chart name and version from the properties Artifactory records when
// it indexes the deployed chart.
type artifactoryServer struct {
	putURL, storageURL, helmRepo string
	// properties are the owner properties as deploy matrix parameters (;key=value...).
	properties string
	owner      []chartProperty
	interval   time.Duration
}

func newArtifactoryServer(spec *HelmRepositoryUploadSpec, path string, owner []chartProperty, interval time.Duration) (*artifactoryServer, error) {
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
		properties: matrixParams(owner),
		owner:      owner,
		interval:   interval,
	}, nil
}

// propertyEscaper escapes the characters Artifactory treats as separators in property values.
var propertyEscaper = strings.NewReplacer(`\`, `\\`, `,`, `\,`, `|`, `\|`, `=`, `\=`, `;`, `\;`)

// matrixParams renders properties as Artifactory deploy matrix parameters, which set them on
// the deployed file in the same request.
func matrixParams(props []chartProperty) string {
	var b strings.Builder
	for _, p := range props {
		// PathEscape keeps '+', which some servers decode as a space; semver build metadata has it.
		b.WriteString(";" + p.key + "=" + strings.ReplaceAll(url.PathEscape(propertyEscaper.Replace(p.value)), "+", "%2B"))
	}
	return b.String()
}

func (a *artifactoryServer) name() string           { return "artifactory" }
func (a *artifactoryServer) helmRepository() string { return a.helmRepo }
func (a *artifactoryServer) uploadURL() string      { return a.putURL }
func (a *artifactoryServer) deployURL() string      { return a.putURL + a.properties }

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
	resp, err := c.do(ctx, http.MethodPut, a.deployURL(), nil, 0, http.Header{
		"X-Checksum-Deploy": {"true"},
		"X-Checksum-Sha256": {sha256Hex},
	})
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// claim reads the file stored at the upload location. A missing file leaves the location free,
// a file with content sha256Hex already holds the chart, and a file whose owner properties name
// this resource is its earlier upload and may be replaced. Any other file is never overwritten,
// because it was stored for another resource, another component version or outside OCM.
func (a *artifactoryServer) claim(ctx context.Context, c *helmClient, sha256Hex string) (bool, error) {
	resp, err := c.do(ctx, http.MethodGet, a.storageURL, nil, -1, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return false, nil
	case http.StatusOK:
	default:
		return false, fmt.Errorf("GET %s returned status %d", redactURL(a.storageURL), resp.StatusCode)
	}
	var info struct {
		Checksums struct {
			SHA256 string `json:"sha256"`
		} `json:"checksums"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxErrorBodyBytes)).Decode(&info); err != nil {
		return false, fmt.Errorf("failed decoding file info of %s: %w", redactURL(a.storageURL), err)
	}
	if sha256Hex != "" && info.Checksums.SHA256 == sha256Hex {
		return true, nil
	}

	keys := make([]string, 0, len(a.owner)+1)
	for _, p := range a.owner {
		keys = append(keys, p.key)
	}
	if !slices.Contains(keys, "ocm.resource.extraIdentity") {
		keys = append(keys, "ocm.resource.extraIdentity")
	}
	props, err := a.readProperties(ctx, c, keys)
	if err != nil {
		return false, err
	}
	want := map[string]string{}
	for _, p := range a.owner {
		want[p.key] = p.value
	}
	for _, key := range keys {
		got := props[key]
		if value, ok := want[key]; ok && (len(got) != 1 || got[0] != value) || !ok && len(got) != 0 {
			return false, fmt.Errorf("%s already stores a file that was not uploaded for this resource (recorded owner: %v); refusing to overwrite it, configure a different path",
				redactURL(a.putURL), props)
		}
	}
	return false, nil
}

// readProperties returns the requested properties of the file at the upload location; a file
// without any of them yields an empty map.
func (a *artifactoryServer) readProperties(ctx context.Context, c *helmClient, keys []string) (map[string][]string, error) {
	target := a.storageURL + "?properties=" + strings.Join(keys, ",")
	resp, err := c.do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return map[string][]string{}, nil
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("GET %s returned status %d", redactURL(target), resp.StatusCode)
	}
	var props struct {
		Properties map[string][]string `json:"properties"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxErrorBodyBytes)).Decode(&props); err != nil {
		return nil, fmt.Errorf("failed decoding properties of %s: %w", redactURL(a.storageURL), err)
	}
	return props.Properties, nil
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
func (n *nexusServer) deployURL() string      { return n.putURL }

// claim leaves the location to Nexus: it stores the chart under a path derived from the chart,
// and its redeploy policy decides whether a stored chart may be replaced.
func (n *nexusServer) claim(context.Context, *helmClient, string) (bool, error) {
	return false, nil
}

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
		if attempt == chartMetadataAttempts {
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
