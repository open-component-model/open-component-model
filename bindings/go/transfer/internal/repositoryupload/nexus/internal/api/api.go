// Package api calls the REST API of a Sonatype Nexus Repository 3 server for one repository:
// the search, the components upload and the files the repository serves.
package api

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
)

// Repository is a repository of a Nexus server.
type Repository struct {
	Client *client.Client
	// Name is the name of the repository.
	Name string
	// URL is where the repository serves its files: <server>/repository/<name>.
	URL string
	// restURL is the base URL of the REST API: <server>/service/rest/v1.
	restURL string
}

// New returns the repository name of the Nexus server at serverURL.
func New(c *client.Client, serverURL, name string) (*Repository, error) {
	repoURL, err := url.JoinPath(serverURL, "repository", name)
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	restURL, err := url.JoinPath(serverURL, "service", "rest", "v1")
	if err != nil {
		return nil, fmt.Errorf("invalid nexus url: %w", err)
	}
	return &Repository{Client: c, Name: name, URL: repoURL, restURL: restURL}, nil
}

// Settings are the settings of a repository.
type Settings struct {
	Format string `json:"format"`
	Type   string `json:"type"`
}

// Settings reads the settings of the repository.
func (r *Repository) Settings(ctx context.Context) (Settings, error) {
	var settings Settings
	err := r.Client.Send(ctx, http.MethodGet, r.restURL+"/repositories/"+url.PathEscape(r.Name), nil, -1, nil, &settings)
	return settings, err
}

// Asset is an asset item of the asset search.
type Asset struct {
	Path string `json:"path"`
	// Checksum maps algorithms (sha1, sha256, sha512, md5) to the hex checksums of the asset.
	Checksum map[string]string `json:"checksum"`
}

// Has reports whether the asset has content d.
func (a Asset) Has(d digest.Digest) bool {
	return d != "" && a.Checksum[d.Algorithm().String()] == d.Encoded()
}

// Component is a component item of the component search.
type Component struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SearchAssets returns all assets of the repository the asset search finds with query,
// following continuation tokens: a name with search wildcards (e.g. *) can match more assets
// than fit on the first page.
func (r *Repository) SearchAssets(ctx context.Context, query url.Values) ([]Asset, error) {
	query.Set("repository", r.Name)
	var assets []Asset
	for {
		var page struct {
			Items             []Asset `json:"items"`
			ContinuationToken string  `json:"continuationToken"`
		}
		if err := r.Client.Send(ctx, http.MethodGet, r.restURL+"/search/assets?"+query.Encode(), nil, -1, nil, &page); err != nil {
			return nil, err
		}
		assets = append(assets, page.Items...)
		if page.ContinuationToken == "" {
			return assets, nil
		}
		query.Set("continuationToken", page.ContinuationToken)
	}
}

// SearchComponents returns the components of the repository the component search finds with
// query. Only the first page is read.
func (r *Repository) SearchComponents(ctx context.Context, query url.Values) ([]Component, error) {
	query.Set("repository", r.Name)
	var page struct {
		Items []Component `json:"items"`
	}
	err := r.Client.Send(ctx, http.MethodGet, r.restURL+"/search?"+query.Encode(), nil, -1, nil, &page)
	return page.Items, err
}

// ChecksumQuery returns the search query selecting the assets with content d. ok is false for an
// empty digest or an algorithm the search does not support.
func ChecksumQuery(d digest.Digest) (url.Values, bool) {
	if d == "" {
		return nil, false
	}
	switch alg := d.Algorithm(); alg {
	case digest.SHA256, digest.SHA512:
		return url.Values{alg.String(): {d.Encoded()}}, true
	}
	return nil, false
}

// StoredFile reports whether the repository stores content known at path (escaped, relative to
// the repository root). query selects the assets by format-specific attributes; when nil, they
// are selected by name. It
// fails when path holds other content, or content whose digest is unknown up front, because
// Nexus records no owner of a file, so a file is never overwritten. Nexus reports the checksum
// of a stored file shortly after storing it, so a file not reported by the search yet is polled
// for. format names the repository format in the error.
func (r *Repository) StoredFile(ctx context.Context, format, path string, query url.Values, known digest.Digest, interval time.Duration) (stored bool, err error) {
	target := r.URL + "/" + path
	resp, err := r.Client.Do(ctx, http.MethodHead, target, nil, -1, nil)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return false, nil
	case http.StatusOK:
	default:
		return false, fmt.Errorf("HEAD %s returned status %d", client.RedactURL(target), resp.StatusCode)
	}
	different := fmt.Errorf("nexus repository %q already stores a different file at %s; the uploader never overwrites files in %s repositories, configure a different path",
		r.Name, client.RedactURL(target), format)
	if known == "" {
		return false, different
	}
	name, err := url.PathUnescape(path)
	if err != nil {
		return false, fmt.Errorf("invalid path %q: %w", path, err)
	}
	// Nexus reports asset paths with a leading slash (e.g. /a/b/c.txt); path never has one.
	assetPath := "/" + name
	if query == nil {
		query = url.Values{"name": {assetPath}}
	}
	var assets []Asset
	if _, err := repositoryupload.Poll(ctx, interval, func() (bool, error) {
		found, err := r.SearchAssets(ctx, query)
		assets = slices.DeleteFunc(found, func(a Asset) bool { return a.Path != assetPath })
		return len(assets) > 0, err
	}); err != nil {
		return false, err
	}
	if slices.ContainsFunc(assets, func(a Asset) bool { return a.Has(known) }) {
		return true, nil
	}
	return false, different
}

// UploadComponent streams content as the single asset assetField of a component with the form
// fields to the components API and returns the digest of the bytes sent, see
// [client.DigestAlgorithm].
func (r *Repository) UploadComponent(ctx context.Context, fields [][2]string, assetField, filename string, content blob.ReadOnlyBlob, known digest.Digest, mediaType string) (digest.Digest, error) {
	rc, err := content.ReadCloser()
	if err != nil {
		return "", fmt.Errorf("failed opening content: %w", err)
	}
	defer func() { _ = rc.Close() }()

	digester := client.DigestAlgorithm(known).Digester()
	body, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	written := make(chan struct{})
	go func() {
		defer close(written)
		pw.CloseWithError(writeComponentForm(form, fields, assetField, filename, io.TeeReader(rc, digester.Hash()), mediaType))
	}()
	target := r.restURL + "/components?" + url.Values{"repository": {r.Name}}.Encode()
	err = r.Client.Send(ctx, http.MethodPost, target, body, -1, http.Header{"Content-Type": {form.FormDataContentType()}}, nil)
	// Unblock the writer if the request stopped reading, then wait until it stopped hashing.
	_ = body.CloseWithError(io.ErrClosedPipe)
	<-written
	return digester.Digest(), err
}

// writeComponentForm writes the components API form of a single asset.
func writeComponentForm(form *multipart.Writer, fields [][2]string, assetField, filename string, content io.Reader, mediaType string) error {
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, assetField, filename))
	header.Set("Content-Type", mediaType)
	part, err := form.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, content); err != nil {
		return err
	}
	return form.Close()
}
