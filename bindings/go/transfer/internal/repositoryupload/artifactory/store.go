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

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/chart"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
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

// store deploys the content to <url>/artifactory/<repository>/<path> with the owner properties.
// Helm charts and npm packages are published with the name and version Artifactory records as
// properties when it indexes the deployed file; other content as the file.
type store struct {
	client                                            *client.Client
	packageType                                       string
	repository, repoURL, putURL, storageURL, helmRepo string
	// properties are the owner properties as deploy matrix parameters (;key=value...).
	properties string
	owner      []property
	interval   time.Duration
	// deployed is the file the last deploy stored, see storedURL.
	deployed deployResponse
}

var _ repositoryupload.Store = (*store)(nil)

func newStore(c *client.Client, spec *uploadv1alpha1.RepositoryUploadSpec, packageType, path string, owner []property, interval time.Duration) (*store, error) {
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
	return &store{
		client:      c,
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

func (s *store) Chart() bool { return s.packageType == "helm" }

func (s *store) URL() string { return s.storedURL() }

// Stored claims the upload location, see claim, and otherwise asks Artifactory to deploy it from
// content it already stores under the SHA-256 checksum, see reuse.
func (s *store) Stored(ctx context.Context, known digest.Digest) (bool, error) {
	stored, err := s.claim(ctx, known)
	if stored || err != nil || known == "" || known.Algorithm() != digest.SHA256 {
		return stored, err
	}
	return s.reuse(ctx, known)
}

func (s *store) Put(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error) {
	header := http.Header{"Content-Type": {mediaType}}
	if known != "" && known.Algorithm() == digest.SHA256 {
		// Artifactory verifies the uploaded bytes against this checksum and rejects the upload
		// on mismatch, so a corrupted stream is never stored.
		header.Set("X-Checksum-Sha256", known.Encoded())
	}
	computed, _, err := s.client.PutBlob(ctx, s.putURL+s.properties, content, known, header, &s.deployed)
	return computed, err
}

func (s *store) Discard(ctx context.Context, _ digest.Digest) error {
	return s.client.Send(ctx, http.MethodDelete, s.storedURL(), nil, -1, nil, nil)
}

// Publish returns a Helm/v1 access on the chart Artifactory indexed (helm), or a Wget/v1 access on
// the stored file; npm packages must have been indexed as such.
func (s *store) Publish(ctx context.Context, _ digest.Digest, mediaType string) (runtime.Typed, error) {
	switch s.packageType {
	case "helm":
		name, version, found, err := s.packageInfo(ctx, "chart.name", "chart.version")
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &repositoryupload.NotRecognizedError{Kind: "a helm chart", Reason: "artifactory recorded no chart name and version for " + client.RedactURL(s.storedURL())}
		}
		return chart.Access("artifactory", s.helmRepo, name, version, client.RedactURL(s.storedURL()))
	case "npm":
		name, version, found, err := s.packageInfo(ctx, "npm.name", "npm.version")
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &repositoryupload.NotRecognizedError{Kind: "an npm package", Reason: "artifactory recorded no npm.name and npm.version for " + client.RedactURL(s.storedURL())}
		}
		slog.InfoContext(ctx, "artifactory indexed the npm package", "url", client.RedactURL(s.storedURL()), "package", name+"@"+version)
	}
	return &wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: s.storedURL(), MediaType: mediaType}, nil
}

// reuse asks Artifactory to deploy the upload URL from content it already stores under the
// checksum ("Deploy Artifact by Checksum"), so the content is not uploaded again. It reports false
// when Artifactory does not have the content (404) or declines the request otherwise; the caller
// then uploads the content, which surfaces real errors such as missing permissions.
func (s *store) reuse(ctx context.Context, sha256 digest.Digest) (bool, error) {
	resp, err := s.client.Do(ctx, http.MethodPut, s.putURL+s.properties, nil, 0, http.Header{
		"X-Checksum-Deploy": {"true"},
		"X-Checksum-Sha256": {sha256.Encoded()},
	})
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil
	}
	var deployed deployResponse
	if err := client.DecodeJSON(resp.Body, &deployed); err != nil {
		return false, fmt.Errorf("failed decoding response of PUT %s: %w", client.RedactURL(s.putURL), err)
	}
	s.deployed = deployed
	return true, nil
}

// deployResponse is the part of an Artifactory deploy response naming the stored file.
type deployResponse struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// storedURL is the URL of the stored file. Artifactory may store a file under another path than
// requested, e.g. a Maven -SNAPSHOT file under its unique timestamped version, so the path of the
// deploy response is used when there is one.
func (s *store) storedURL() string {
	if s.deployed.Repo != s.repository || s.deployed.Path == "" {
		return s.putURL
	}
	segments := strings.Split(strings.TrimPrefix(s.deployed.Path, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return s.repoURL + "/" + strings.Join(segments, "/")
}

// claim reads the file stored at the upload location. A missing file leaves the location free,
// a file with content known already holds the content, and a file whose owner properties name
// this resource is its earlier upload and may be replaced. Any other file is never overwritten,
// because it was stored for another resource, another component version or outside OCM.
func (s *store) claim(ctx context.Context, known digest.Digest) (bool, error) {
	var info struct {
		// Checksums maps algorithms (sha1, sha256, md5) to the hex checksums of the file.
		Checksums map[string]string `json:"checksums"`
	}
	if exists, err := s.client.GetJSON(ctx, s.storageURL, &info); !exists || err != nil {
		return false, err
	}
	if known != "" && info.Checksums[known.Algorithm().String()] == known.Encoded() {
		return true, nil
	}

	keys := make([]string, 0, len(s.owner)+1)
	want := map[string]string{}
	for _, p := range s.owner {
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
	if _, err := s.client.GetJSON(ctx, s.storageURL+"?properties="+strings.Join(keys, ","), &props); err != nil {
		return false, err
	}
	for _, key := range keys {
		got := props.Properties[key]
		if value, ok := want[key]; ok && (len(got) != 1 || got[0] != value) || !ok && len(got) != 0 {
			return false, fmt.Errorf("%s already stores a file that was not uploaded for this resource (recorded owner: %v); refusing to overwrite it, configure a different path",
				client.RedactURL(s.putURL), props.Properties)
		}
	}
	return false, nil
}

// packageInfo reads the package name and version Artifactory records as the properties nameKey
// and versionKey when it indexes the stored file. It polls in case the metadata is calculated
// asynchronously and reports found=false when Artifactory recorded none, i.e. did not recognize
// the content as a package of the repository type.
func (s *store) packageInfo(ctx context.Context, nameKey, versionKey string) (name, version string, found bool, err error) {
	target := s.storageURL + "?properties=" + nameKey + "," + versionKey
	found, err = repositoryupload.Poll(ctx, s.interval, func() (bool, error) {
		var props struct {
			Properties map[string][]string `json:"properties"`
		}
		if ok, err := s.client.GetJSON(ctx, target, &props); !ok || err != nil {
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
