// Package api calls the REST API of a JFrog Artifactory server for one repository: its
// configuration, and the deploy, storage and property endpoints of the files it stores.
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// Repository is a repository of an Artifactory server.
type Repository struct {
	Client *client.Client
	// Name is the key of the repository.
	Name string
	// URL is where the repository serves its files: <server>/artifactory/<name>.
	URL string
	// HelmURL is the Helm repository API of the repository: <server>/artifactory/api/helm/<name>.
	HelmURL string
	// apiURL is the base URL of the REST API: <server>/artifactory/api.
	apiURL string
}

// URLs returns the URLs of repository name of the Artifactory server at serverURL, the URL without
// the /artifactory segment: where it serves its files (<server>/artifactory/<name>) and its Helm
// repository API (<server>/artifactory/api/helm/<name>).
func URLs(serverURL, name string) (repoURL, helmURL string, err error) {
	repoURL, err = url.JoinPath(serverURL, "artifactory", name)
	if err != nil {
		return "", "", fmt.Errorf("invalid artifactory url: %w", err)
	}
	apiURL, err := url.JoinPath(serverURL, "artifactory", "api")
	if err != nil {
		return "", "", fmt.Errorf("invalid artifactory url: %w", err)
	}
	return repoURL, apiURL + "/helm/" + url.PathEscape(name), nil
}

// New returns the repository name of the Artifactory server at serverURL, the URL without the
// /artifactory segment.
func New(c *client.Client, serverURL, name string) (*Repository, error) {
	repoURL, helmURL, err := URLs(serverURL, name)
	if err != nil {
		return nil, err
	}
	apiURL, err := url.JoinPath(serverURL, "artifactory", "api")
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	return &Repository{Client: c, Name: name, URL: repoURL, HelmURL: helmURL, apiURL: apiURL}, nil
}

// Configuration is the configuration of a repository.
type Configuration struct {
	PackageType string `json:"packageType"`
	RClass      string `json:"rclass"`
}

// Configuration reads the configuration of the repository.
func (r *Repository) Configuration(ctx context.Context) (Configuration, error) {
	var config Configuration
	err := r.Client.Send(ctx, http.MethodGet, r.apiURL+"/repositories/"+url.PathEscape(r.Name), nil, -1, nil, &config)
	return config, err
}

// Property is a property Artifactory records on a deployed file.
type Property struct {
	Key, Value string
}

// Owner returns the properties that identify the resource content is uploaded for.
func Owner(cv *uploadv1alpha1.RepositoryUploadComponentVersion, res *descriptor.Resource) []Property {
	owner := []Property{
		{"ocm.component.name", cv.Component},
		{"ocm.component.version", cv.Version},
		{"ocm.resource.name", res.Name},
		{"ocm.resource.version", res.Version},
	}
	if len(res.ExtraIdentity) > 0 {
		owner = append(owner, Property{"ocm.resource.extraIdentity", res.ExtraIdentity.String()})
	}
	return owner
}

// File is a file of the repository that content is deployed to with owner properties. A file is
// only replaced when its owner properties name the same resource or it has the same content, see
// [File.Stored]. File implements all of [repositoryupload.Store] but Chart and Publish.
type File struct {
	repo               *Repository
	putURL, storageURL string
	owner              []Property
	interval           time.Duration
	// matrixParams are the owner properties as deploy matrix parameters (;key=value...).
	matrixParams string
	// deployed is the file the last deploy stored, see URL.
	deployed deployResponse
}

// File returns the file at path (escaped, relative to the repository root) owned by owner.
// interval is the wait between two polls of the file properties, see [File.Package].
func (r *Repository) File(path string, owner []Property, interval time.Duration) *File {
	return &File{
		repo:         r,
		putURL:       r.URL + "/" + path,
		storageURL:   r.apiURL + "/storage/" + url.PathEscape(r.Name) + "/" + path,
		owner:        owner,
		interval:     interval,
		matrixParams: matrixParams(owner),
	}
}

// propertyEscaper escapes the characters Artifactory treats as separators in property values.
var propertyEscaper = strings.NewReplacer(`\`, `\\`, `,`, `\,`, `|`, `\|`, `=`, `\=`, `;`, `\;`)

// matrixParams renders properties as deploy matrix parameters, which set them on the deployed
// file in the same request.
func matrixParams(props []Property) string {
	var b strings.Builder
	for _, p := range props {
		// PathEscape keeps '+', which some servers decode as a space; semver build metadata has it.
		b.WriteString(";" + p.Key + "=" + strings.ReplaceAll(url.PathEscape(propertyEscaper.Replace(p.Value)), "+", "%2B"))
	}
	return b.String()
}

// URL is the URL of the stored file. Artifactory may store a file under another path than
// requested, e.g. a Maven -SNAPSHOT file under its unique timestamped version, so the path of the
// deploy response is used when there is one.
func (f *File) URL() string {
	if f.deployed.Repo != f.repo.Name || f.deployed.Path == "" {
		return f.putURL
	}
	segments := strings.Split(strings.TrimPrefix(f.deployed.Path, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return f.repo.URL + "/" + strings.Join(segments, "/")
}

// Stored claims the file, see claim, and otherwise asks Artifactory to deploy it from content it
// already stores under the SHA-256 checksum, see reuse.
func (f *File) Stored(ctx context.Context, known digest.Digest) (bool, error) {
	stored, err := f.claim(ctx, known)
	if stored || err != nil || known == "" || known.Algorithm() != digest.SHA256 {
		return stored, err
	}
	return f.reuse(ctx, known)
}

// Put deploys content with the owner properties.
func (f *File) Put(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error) {
	header := http.Header{"Content-Type": {mediaType}}
	if known != "" && known.Algorithm() == digest.SHA256 {
		// Artifactory verifies the uploaded bytes against this checksum and rejects the upload
		// on mismatch, so a corrupted stream is never stored.
		header.Set("X-Checksum-Sha256", known.Encoded())
	}
	computed, _, err := f.repo.Client.PutBlob(ctx, f.putURL+f.matrixParams, content, known, header, &f.deployed)
	return computed, err
}

// Discard deletes the stored file.
func (f *File) Discard(ctx context.Context, _ digest.Digest) error {
	return f.repo.Client.Send(ctx, http.MethodDelete, f.URL(), nil, -1, nil, nil)
}

// Package reads the package name and version Artifactory records as the properties nameKey and
// versionKey when it indexes the stored file. It polls in case the metadata is calculated
// asynchronously and reports found=false when Artifactory recorded none, i.e. did not recognize
// the content as a package of the repository type.
func (f *File) Package(ctx context.Context, nameKey, versionKey string) (name, version string, found bool, err error) {
	target := f.storageURL + "?properties=" + nameKey + "," + versionKey
	found, err = repositoryupload.Poll(ctx, f.interval, func() (bool, error) {
		var props properties
		if ok, err := f.repo.Client.GetJSON(ctx, target, &props); !ok || err != nil {
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

// deployResponse is the part of a deploy response naming the stored file.
type deployResponse struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// properties is a properties response of the storage API.
type properties struct {
	Properties map[string][]string `json:"properties"`
}

// reuse asks Artifactory to deploy the file from content it already stores under the checksum
// ("Deploy Artifact by Checksum"), so the content is not uploaded again. It reports false when
// Artifactory does not have the content (404) or declines the request otherwise; the caller then
// uploads the content, which surfaces real errors such as missing permissions.
func (f *File) reuse(ctx context.Context, sha256 digest.Digest) (bool, error) {
	resp, err := f.repo.Client.Do(ctx, http.MethodPut, f.putURL+f.matrixParams, nil, 0, http.Header{
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
		return false, fmt.Errorf("failed decoding response of PUT %s: %w", client.RedactURL(f.putURL), err)
	}
	f.deployed = deployed
	return true, nil
}

// claim reads the stored file. A missing file leaves the location free, a file with content
// known already holds the content, and a file whose owner properties name this resource is its
// earlier upload and may be replaced. Any other file is never overwritten, because it was stored
// for another resource, another component version or outside OCM.
func (f *File) claim(ctx context.Context, known digest.Digest) (bool, error) {
	var info struct {
		// Checksums maps algorithms (sha1, sha256, md5) to the hex checksums of the file.
		Checksums map[string]string `json:"checksums"`
	}
	if exists, err := f.repo.Client.GetJSON(ctx, f.storageURL, &info); !exists || err != nil {
		return false, err
	}
	if known != "" && info.Checksums[known.Algorithm().String()] == known.Encoded() {
		return true, nil
	}

	keys := make([]string, 0, len(f.owner)+1)
	want := map[string]string{}
	for _, p := range f.owner {
		keys = append(keys, p.Key)
		want[p.Key] = p.Value
	}
	if !slices.Contains(keys, "ocm.resource.extraIdentity") {
		keys = append(keys, "ocm.resource.extraIdentity")
	}
	var props properties
	// A file without any of the properties yields 404.
	if _, err := f.repo.Client.GetJSON(ctx, f.storageURL+"?properties="+strings.Join(keys, ","), &props); err != nil {
		return false, err
	}
	for _, key := range keys {
		got := props.Properties[key]
		if value, ok := want[key]; ok && (len(got) != 1 || got[0] != value) || !ok && len(got) != 0 {
			return false, fmt.Errorf("%s already stores a file that was not uploaded for this resource (recorded owner: %v); refusing to overwrite it, configure a different path",
				client.RedactURL(f.putURL), props.Properties)
		}
	}
	return false, nil
}
