package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/blob/compression"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// helmRepositoryServer is the server-specific part of a Helm chart upload, see uploadHelm.
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
	claim(ctx context.Context, c *repositoryClient, sha256Hex string) (stored bool, err error)
	// reuse makes content the repository already stores under sha256Hex available without uploading it.
	reuse(ctx context.Context, c *repositoryClient, sha256Hex string) (bool, error)
	// rejectedUploadStored reports whether the repository stores the content of an upload it rejected.
	rejectedUploadStored(ctx context.Context, c *repositoryClient, sha256Hex string) bool
	// chart returns the chart name and version the server recorded for the stored content;
	// isChart=false when it recorded none, i.e. did not recognize the content as a chart.
	chart(ctx context.Context, c *repositoryClient, sha256Hex string) (name, version string, isChart bool, err error)
	// discard removes uploaded content that must not be published.
	discard(ctx context.Context, c *repositoryClient, sha256Hex string) error
}

// chartProperty is an Artifactory property recorded on a deployed file.
type chartProperty struct {
	key, value string
}

// uploadHelm uploads the packaged chart located in the resource content to a Helm repository and
// returns the resource with a Helm/v1 access on it. The chart is not parsed: its name and version
// are the chart metadata the server records for the uploaded chart. Where the chart is stored
// and how the metadata is read depends on the server, see [helmRepositoryServer].
func (u *repositoryUploader) uploadHelm(ctx context.Context, c *repositoryClient, spec *RepositoryUploadSpec, src *descriptor.Resource, srv helmRepositoryServer) (*descriptor.Resource, error) {
	req, err := u.open(ctx, spec, src)
	if err != nil {
		return nil, err
	}
	chart, err := u.Charts.Open(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = chart.Close() }()

	expected, known, err := knownDigest(src.Digest, chart.Archive, chart.FromOCI)
	if err != nil {
		return nil, err
	}

	// The upload location must be free, hold this resource's earlier upload, or already hold
	// the chart; nothing else is ever overwritten.
	reused, err := srv.claim(ctx, c, known)
	if err != nil {
		return nil, err
	}
	if !reused && known != "" {
		if reused, err = srv.reuse(ctx, c, known); err != nil {
			return nil, err
		}
	}
	digestHex := known
	uploadURL := redactURL(srv.uploadURL())
	if reused {
		slog.InfoContext(ctx, "reused helm chart content already stored in the helm repository",
			"server", srv.name(), "resource", src.ToIdentity(), "url", uploadURL)
	} else {
		computed, complete, err := uploadBlob(ctx, c, chart.Archive, srv.deployURL(), srv.uploadHeader(known), nil)
		switch {
		case err != nil:
			// A repository that rejects redeploying a chart still stores its content, e.g.
			// from an earlier transfer of the same resource without a source digest.
			if known != "" || !complete || !srv.rejectedUploadStored(ctx, c, computed) {
				return nil, err
			}
			slog.InfoContext(ctx, "helm repository already stores the chart the upload was rejected for",
				"server", srv.name(), "resource", src.ToIdentity(), "url", uploadURL)
		case known != "" && computed != known:
			if err := srv.discard(ctx, c, computed); err != nil {
				slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
			}
			return nil, fmt.Errorf("digest mismatch: expected %s, got %s", known, computed)
		default:
			slog.InfoContext(ctx, "uploaded helm chart", "server", srv.name(), "resource", src.ToIdentity(), "url", uploadURL)
		}
		digestHex = computed
	}

	// The upload succeeded, so the server stores the content either way; isChart only
	// reports whether it recognized that content as a helm chart and recorded its metadata.
	name, version, isChart, err := srv.chart(ctx, c, digestHex)
	if err != nil {
		return nil, err
	}
	if !isChart {
		// Remove the stored non-chart content again (Artifactory deletes the uploaded file;
		// Nexus cannot, see nexusServer.discard).
		if err := srv.discard(ctx, c, digestHex); err != nil {
			slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
		}
		return nil, fmt.Errorf("content of resource %s is not a helm chart: %s recorded no chart name and version for %s", src.ToIdentity(), srv.name(), uploadURL)
	}
	if strings.ContainsAny(name, ":/") || strings.Contains(version, "/") {
		return nil, fmt.Errorf("%s recorded an invalid chart name %q or version %q for %s", srv.name(), name, version, uploadURL)
	}

	out := src.DeepCopy()
	out.Access = &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: srv.helmRepository(),
		HelmChart:      name + ":" + version,
	}
	out.Digest = uploadedDigest(src.Digest, expected, digestHex)
	return out, nil
}

// artifactoryServer deploys the chart to <url>/artifactory/<repository>/<path> with the owner
// properties and reads the chart name and version from the properties Artifactory records when
// it indexes the deployed chart.
type artifactoryServer struct {
	repository, repoURL, putURL, storageURL, helmRepo string
	// properties are the owner properties as deploy matrix parameters (;key=value...).
	properties string
	owner      []chartProperty
	interval   time.Duration
	// deployed is the file the last deploy stored, see storedURL.
	deployed artifactoryDeployment
}

func newArtifactoryServer(spec *RepositoryUploadSpec, path string, owner []chartProperty, interval time.Duration) (*artifactoryServer, error) {
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
		repository: spec.Repository,
		repoURL:    uploadBase,
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
func (a *artifactoryServer) reuse(ctx context.Context, c *repositoryClient, sha256Hex string) (bool, error) {
	resp, err := c.do(ctx, http.MethodPut, a.deployURL(), nil, 0, http.Header{
		"X-Checksum-Deploy": {"true"},
		"X-Checksum-Sha256": {sha256Hex},
	})
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil
	}
	var deployed artifactoryDeployment
	if err := decodeJSONBody(resp.Body, &deployed); err != nil {
		return false, fmt.Errorf("failed decoding response of PUT %s: %w", redactURL(a.putURL), err)
	}
	a.deployed = deployed
	return true, nil
}

// artifactoryDeployment is the part of an Artifactory deploy response naming the stored file.
type artifactoryDeployment struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// storedURL is the URL of the stored file. Artifactory may store a file under another path than
// requested, e.g. a Maven -SNAPSHOT file under its unique timestamped version, so the path of the
// deploy response is used when there is one.
func (a *artifactoryServer) storedURL() string {
	if a.deployed.Repo != a.repository || a.deployed.Path == "" {
		return a.putURL
	}
	segments := strings.Split(strings.TrimPrefix(a.deployed.Path, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return a.repoURL + "/" + strings.Join(segments, "/")
}

// claim reads the file stored at the upload location. A missing file leaves the location free,
// a file with content sha256Hex already holds the chart, and a file whose owner properties name
// this resource is its earlier upload and may be replaced. Any other file is never overwritten,
// because it was stored for another resource, another component version or outside OCM.
func (a *artifactoryServer) claim(ctx context.Context, c *repositoryClient, sha256Hex string) (bool, error) {
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
func (a *artifactoryServer) readProperties(ctx context.Context, c *repositoryClient, keys []string) (map[string][]string, error) {
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
func (a *artifactoryServer) rejectedUploadStored(context.Context, *repositoryClient, string) bool {
	return false
}

// chart reads the chart name and version Artifactory records as properties of the deployed chart.
// It polls briefly in case the metadata is calculated asynchronously and reports found=false when
// Artifactory recorded none, i.e. the content is not a helm chart.
func (a *artifactoryServer) chart(ctx context.Context, c *repositoryClient, _ string) (string, string, bool, error) {
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

func (a *artifactoryServer) discard(ctx context.Context, c *repositoryClient, _ string) error {
	return c.send(ctx, http.MethodDelete, a.storedURL(), nil, -1, nil, nil)
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

func newNexusServer(spec *RepositoryUploadSpec, file string, interval time.Duration) (*nexusServer, error) {
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
func (n *nexusServer) claim(context.Context, *repositoryClient, string) (bool, error) {
	return false, nil
}

// uploadHeader carries no checksum: Nexus does not verify one, the caller compares after upload.
func (n *nexusServer) uploadHeader(string) http.Header {
	return http.Header{"Content-Type": {compression.MediaTypeGzip}}
}

// reuse reports whether the repository already stores a chart with the content, which Nexus then
// publishes under its own path, so nothing needs to be uploaded.
func (n *nexusServer) reuse(ctx context.Context, c *repositoryClient, sha256Hex string) (bool, error) {
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
func (n *nexusServer) rejectedUploadStored(ctx context.Context, c *repositoryClient, sha256Hex string) bool {
	_, _, found, err := n.chart(ctx, c, sha256Hex)
	return err == nil && found
}

// chart polls the component search until it finds the content and fails when the content is
// stored as more than one chart name and version.
func (n *nexusServer) chart(ctx context.Context, c *repositoryClient, sha256Hex string) (string, string, bool, error) {
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
func (n *nexusServer) discard(_ context.Context, _ *repositoryClient, sha256Hex string) error {
	return fmt.Errorf("nexus stores charts under a path derived from the chart, so content with SHA-256 %s is left in repository %s", sha256Hex, n.repository)
}

// search returns the helm components of the repository whose asset has the SHA-256. Only the
// first page is read: one content is expected to be stored as at most one component.
func (n *nexusServer) search(ctx context.Context, c *repositoryClient, sha256Hex string) ([]nexusComponent, error) {
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
