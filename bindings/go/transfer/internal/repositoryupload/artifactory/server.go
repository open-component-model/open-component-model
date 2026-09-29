package artifactory

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
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
)

// property is an Artifactory property recorded on a deployed file.
type property struct {
	key, value string
}

// ownerProperties returns the properties that identify the resource content is uploaded for.
func ownerProperties(cv *repositoryupload.ComponentVersion, res *descriptor.Resource) []property {
	owner := []property{
		{"ocm.component.name", cv.Component},
		{"ocm.component.version", cv.Version},
		{"ocm.resource.name", res.Name},
		{"ocm.resource.version", res.Version},
	}
	if len(res.ExtraIdentity) > 0 {
		owner = append(owner, property{"ocm.resource.extraIdentity", res.ExtraIdentity.String()})
	}
	return owner
}

// server deploys the chart to <url>/artifactory/<repository>/<path> with the owner
// properties and reads the chart name and version from the properties Artifactory records when
// it indexes the deployed chart.
type server struct {
	repository, repoURL, putURL, storageURL, helmRepo string
	// properties are the owner properties as deploy matrix parameters (;key=value...).
	properties string
	owner      []property
	interval   time.Duration
	// deployed is the file the last deploy stored, see storedURL.
	deployed deployment
}

func newServer(spec *repositoryupload.Spec, path string, owner []property, interval time.Duration) (*server, error) {
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
	return &server{
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
func matrixParams(props []property) string {
	var b strings.Builder
	for _, p := range props {
		// PathEscape keeps '+', which some servers decode as a space; semver build metadata has it.
		b.WriteString(";" + p.key + "=" + strings.ReplaceAll(url.PathEscape(propertyEscaper.Replace(p.value)), "+", "%2B"))
	}
	return b.String()
}

func (a *server) Name() string           { return "artifactory" }
func (a *server) HelmRepository() string { return a.helmRepo }
func (a *server) UploadURL() string      { return a.putURL }
func (a *server) DeployURL() string      { return a.putURL + a.properties }

func (a *server) UploadHeader(sha256Hex string) http.Header {
	header := http.Header{"Content-Type": {compression.MediaTypeGzip}}
	if sha256Hex != "" {
		// Artifactory verifies the uploaded bytes against this checksum and rejects the upload
		// on mismatch, so a corrupted stream is never stored.
		header.Set("X-Checksum-Sha256", sha256Hex)
	}
	return header
}

// Reuse asks Artifactory to deploy the upload URL from content it already stores under the
// checksum ("Deploy Artifact by Checksum"), so the chart is not uploaded again. It reports false
// when Artifactory does not have the content (404) or declines the request otherwise; the caller
// then uploads the chart, which surfaces real errors such as missing permissions.
func (a *server) Reuse(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	resp, err := c.Do(ctx, http.MethodPut, a.DeployURL(), nil, 0, http.Header{
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
	var deployed deployment
	if err := repositoryupload.DecodeJSONBody(resp.Body, &deployed); err != nil {
		return false, fmt.Errorf("failed decoding response of PUT %s: %w", repositoryupload.RedactURL(a.putURL), err)
	}
	a.deployed = deployed
	return true, nil
}

// deployment is the part of an Artifactory deploy response naming the stored file.
type deployment struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// storedURL is the URL of the stored file. Artifactory may store a file under another path than
// requested, e.g. a Maven -SNAPSHOT file under its unique timestamped version, so the path of the
// deploy response is used when there is one.
func (a *server) storedURL() string {
	if a.deployed.Repo != a.repository || a.deployed.Path == "" {
		return a.putURL
	}
	segments := strings.Split(strings.TrimPrefix(a.deployed.Path, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return a.repoURL + "/" + strings.Join(segments, "/")
}

// Claim reads the file stored at the upload location. A missing file leaves the location free,
// a file with content sha256Hex already holds the chart, and a file whose owner properties name
// this resource is its earlier upload and may be replaced. Any other file is never overwritten,
// because it was stored for another resource, another component version or outside OCM.
func (a *server) Claim(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	resp, err := c.Do(ctx, http.MethodGet, a.storageURL, nil, -1, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return false, nil
	case http.StatusOK:
	default:
		return false, fmt.Errorf("GET %s returned status %d", repositoryupload.RedactURL(a.storageURL), resp.StatusCode)
	}
	var info struct {
		Checksums struct {
			SHA256 string `json:"sha256"`
		} `json:"checksums"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, repositoryupload.MaxErrorBodyBytes)).Decode(&info); err != nil {
		return false, fmt.Errorf("failed decoding file info of %s: %w", repositoryupload.RedactURL(a.storageURL), err)
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
				repositoryupload.RedactURL(a.putURL), props)
		}
	}
	return false, nil
}

// readProperties returns the requested properties of the file at the upload location; a file
// without any of them yields an empty map.
func (a *server) readProperties(ctx context.Context, c *repositoryupload.Client, keys []string) (map[string][]string, error) {
	target := a.storageURL + "?properties=" + strings.Join(keys, ",")
	resp, err := c.Do(ctx, http.MethodGet, target, nil, -1, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return map[string][]string{}, nil
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("GET %s returned status %d", repositoryupload.RedactURL(target), resp.StatusCode)
	}
	var props struct {
		Properties map[string][]string `json:"properties"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, repositoryupload.MaxErrorBodyBytes)).Decode(&props); err != nil {
		return nil, fmt.Errorf("failed decoding properties of %s: %w", repositoryupload.RedactURL(a.storageURL), err)
	}
	return props.Properties, nil
}

// RejectedUploadStored is false: a rejected Artifactory upload fails the transfer.
func (a *server) RejectedUploadStored(context.Context, *repositoryupload.Client, string) bool {
	return false
}

// Chart reads the chart name and version Artifactory records as properties of the deployed chart.
// It polls briefly in case the metadata is calculated asynchronously and reports found=false when
// Artifactory recorded none, i.e. the content is not a helm chart.
func (a *server) Chart(ctx context.Context, c *repositoryupload.Client, _ string) (string, string, bool, error) {
	return a.packageInfo(ctx, c, "chart.name", "chart.version")
}

// packageInfo reads the package name and version Artifactory records as the properties nameKey
// and versionKey when it indexes the stored file. It polls briefly in case the metadata is
// calculated asynchronously and reports found=false when Artifactory recorded none, i.e. did
// not recognize the content as a package of the repository type.
func (a *server) packageInfo(ctx context.Context, c *repositoryupload.Client, nameKey, versionKey string) (string, string, bool, error) {
	target := a.storageURL + "?properties=" + nameKey + "," + versionKey
	for attempt := 1; ; attempt++ {
		resp, err := c.Do(ctx, http.MethodGet, target, nil, -1, nil)
		if err != nil {
			return "", "", false, err
		}
		var props struct {
			Properties map[string][]string `json:"properties"`
		}
		switch resp.StatusCode {
		case http.StatusOK:
			err = json.NewDecoder(io.LimitReader(resp.Body, repositoryupload.MaxErrorBodyBytes)).Decode(&props)
			_ = resp.Body.Close()
			if err != nil {
				return "", "", false, fmt.Errorf("failed decoding package properties of %s: %w", repositoryupload.RedactURL(a.storageURL), err)
			}
			if names, versions := props.Properties[nameKey], props.Properties[versionKey]; len(names) == 1 && len(versions) == 1 {
				return names[0], versions[0], names[0] != "" && versions[0] != "", nil
			}
		case http.StatusNotFound:
			_ = resp.Body.Close()
		default:
			_ = resp.Body.Close()
			return "", "", false, fmt.Errorf("GET %s returned status %d", repositoryupload.RedactURL(target), resp.StatusCode)
		}
		if attempt == repositoryupload.PollAttempts {
			return "", "", false, nil
		}
		select {
		case <-ctx.Done():
			return "", "", false, ctx.Err()
		case <-time.After(a.interval):
		}
	}
}

func (a *server) Discard(ctx context.Context, c *repositoryupload.Client, _ string) error {
	return c.Send(ctx, http.MethodDelete, a.storedURL(), nil, -1, nil, nil)
}
