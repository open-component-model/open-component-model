package artifactory

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// property is an Artifactory property recorded on a deployed file.
type property struct {
	key, value string
}

// ownerProperties returns the properties that identify the resource content is uploaded for.
func ownerProperties(cv *uploadv1alpha1.RepositoryUploadComponentVersion, res *descriptor.Resource) []property {
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

// server deploys the content to <url>/artifactory/<repository>/<path> with the owner properties.
// Helm charts and npm packages are published with the name and version Artifactory records as
// properties when it indexes the deployed file; other content as the file.
type server struct {
	packageType                                       string
	repository, repoURL, putURL, storageURL, helmRepo string
	// properties are the owner properties as deploy matrix parameters (;key=value...).
	properties string
	owner      []property
	interval   time.Duration
	// deployed is the file the last deploy stored, see storedURL.
	deployed deployment
}

var _ repositoryupload.Store = (*server)(nil)

func newServer(spec *uploadv1alpha1.RepositoryUploadSpec, packageType, path string, owner []property, interval time.Duration) (*server, error) {
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
		packageType: packageType,
		repository:  spec.Repository,
		repoURL:     uploadBase,
		putURL:      uploadBase + "/" + path,
		storageURL:  storageBase + "/" + path,
		helmRepo:    helmRepo,
		properties:  matrixParams(owner),
		owner:       owner,
		interval:    interval,
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

func (a *server) Chart() bool { return a.packageType == "helm" }

func (a *server) URL() string { return a.storedURL() }

// Stored claims the upload location, see claim, and otherwise asks Artifactory to deploy it from
// content it already stores under the checksum, see reuse.
func (a *server) Stored(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	stored, err := a.claim(ctx, c, sha256Hex)
	if stored || err != nil || sha256Hex == "" {
		return stored, err
	}
	return a.reuse(ctx, c, sha256Hex)
}

func (a *server) Put(ctx context.Context, c *repositoryupload.Client, content blob.ReadOnlyBlob, mediaType, sha256Hex string) (string, error) {
	header := http.Header{"Content-Type": {mediaType}}
	if sha256Hex != "" {
		// Artifactory verifies the uploaded bytes against this checksum and rejects the upload
		// on mismatch, so a corrupted stream is never stored.
		header.Set("X-Checksum-Sha256", sha256Hex)
	}
	computed, _, err := repositoryupload.UploadBlob(ctx, c, content, a.putURL+a.properties, header, &a.deployed)
	return computed, err
}

func (a *server) Discard(ctx context.Context, c *repositoryupload.Client, _ string) error {
	return c.Send(ctx, http.MethodDelete, a.storedURL(), nil, -1, nil, nil)
}

// Publish returns a Helm/v1 access on the chart Artifactory indexed (helm), or a Wget/v1 access on
// the stored file; npm packages must have been indexed as such.
func (a *server) Publish(ctx context.Context, c *repositoryupload.Client, _, mediaType string) (runtime.Typed, error) {
	switch a.packageType {
	case "helm":
		name, version, found, err := a.packageInfo(ctx, c, "chart.name", "chart.version")
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &repositoryupload.NotRecognizedError{Kind: "a helm chart", Reason: "artifactory recorded no chart name and version for " + repositoryupload.RedactURL(a.storedURL())}
		}
		return repositoryupload.HelmAccess("artifactory", a.helmRepo, name, version, repositoryupload.RedactURL(a.storedURL()))
	case "npm":
		name, version, found, err := a.packageInfo(ctx, c, "npm.name", "npm.version")
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &repositoryupload.NotRecognizedError{Kind: "an npm package", Reason: "artifactory recorded no npm.name and npm.version for " + repositoryupload.RedactURL(a.storedURL())}
		}
		slog.InfoContext(ctx, "artifactory indexed the npm package", "url", repositoryupload.RedactURL(a.storedURL()), "package", name+"@"+version)
	}
	return &wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: a.storedURL(), MediaType: mediaType}, nil
}

// reuse asks Artifactory to deploy the upload URL from content it already stores under the
// checksum ("Deploy Artifact by Checksum"), so the content is not uploaded again. It reports false
// when Artifactory does not have the content (404) or declines the request otherwise; the caller
// then uploads the content, which surfaces real errors such as missing permissions.
func (a *server) reuse(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	resp, err := c.Do(ctx, http.MethodPut, a.putURL+a.properties, nil, 0, http.Header{
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

// claim reads the file stored at the upload location. A missing file leaves the location free,
// a file with content sha256Hex already holds the content, and a file whose owner properties name
// this resource is its earlier upload and may be replaced. Any other file is never overwritten,
// because it was stored for another resource, another component version or outside OCM.
func (a *server) claim(ctx context.Context, c *repositoryupload.Client, sha256Hex string) (bool, error) {
	var info struct {
		Checksums struct {
			SHA256 string `json:"sha256"`
		} `json:"checksums"`
	}
	if exists, err := c.GetJSON(ctx, a.storageURL, &info); !exists || err != nil {
		return false, err
	}
	if sha256Hex != "" && info.Checksums.SHA256 == sha256Hex {
		return true, nil
	}

	keys := make([]string, 0, len(a.owner)+1)
	want := map[string]string{}
	for _, p := range a.owner {
		keys = append(keys, p.key)
		want[p.key] = p.value
	}
	if !slices.Contains(keys, "ocm.resource.extraIdentity") {
		keys = append(keys, "ocm.resource.extraIdentity")
	}
	var props struct {
		Properties map[string][]string `json:"properties"`
	}
	// A file without any of the properties yields 404.
	if _, err := c.GetJSON(ctx, a.storageURL+"?properties="+strings.Join(keys, ","), &props); err != nil {
		return false, err
	}
	for _, key := range keys {
		got := props.Properties[key]
		if value, ok := want[key]; ok && (len(got) != 1 || got[0] != value) || !ok && len(got) != 0 {
			return false, fmt.Errorf("%s already stores a file that was not uploaded for this resource (recorded owner: %v); refusing to overwrite it, configure a different path",
				repositoryupload.RedactURL(a.putURL), props.Properties)
		}
	}
	return false, nil
}

// packageInfo reads the package name and version Artifactory records as the properties nameKey
// and versionKey when it indexes the stored file. It polls in case the metadata is calculated
// asynchronously and reports found=false when Artifactory recorded none, i.e. did not recognize
// the content as a package of the repository type.
func (a *server) packageInfo(ctx context.Context, c *repositoryupload.Client, nameKey, versionKey string) (name, version string, found bool, err error) {
	target := a.storageURL + "?properties=" + nameKey + "," + versionKey
	found, err = repositoryupload.Poll(ctx, a.interval, func() (bool, error) {
		var props struct {
			Properties map[string][]string `json:"properties"`
		}
		if ok, err := c.GetJSON(ctx, target, &props); !ok || err != nil {
			return false, err
		}
		names, versions := props.Properties[nameKey], props.Properties[versionKey]
		if len(names) != 1 || len(versions) != 1 {
			return false, nil
		}
		name, version = names[0], versions[0]
		return true, nil
	})
	return name, version, found && name != "" && version != "", err
}
