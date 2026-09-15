package resource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/pypi/internal"
	"ocm.software/open-component-model/bindings/go/pypi/internal/pypi"
	pypiaccess "ocm.software/open-component-model/bindings/go/pypi/spec/access"
	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	credsv1 "ocm.software/open-component-model/bindings/go/pypi/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MediaTypeTGZ is the media type of every download.
const MediaTypeTGZ = "application/x-tgz"

// ResourceRepository downloads and uploads PyPI distribution files over HTTP(S).
type ResourceRepository struct {
	client *pypi.Client
}

// Option configures a ResourceRepository.
type Option func(*ResourceRepository)

// WithHTTPClient sets the HTTP client used for download and upload. Defaults
// to http.DefaultClient; the CLI passes its configured client so timeouts,
// retries and TLS settings apply.
func WithHTTPClient(c *http.Client) Option {
	return func(r *ResourceRepository) { r.client = pypi.NewClient(c) }
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a PyPI ResourceRepository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	r := &ResourceRepository{}
	for _, opt := range opts {
		opt(r)
	}
	if r.client == nil {
		r.client = pypi.NewClient(nil)
	}
	return r
}

// GetResourceRepositoryScheme returns the PyPI access scheme.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return pypiaccess.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the "PyPIRepository" identity for the resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	p, err := internal.ConvertAccess(resource)
	if err != nil {
		return nil, err
	}
	return internal.CredentialConsumerIdentity(p.IndexURL)
}

// DownloadResource resolves the distribution files the access spec selects from
// the Simple Repository API index and packages them, each followed by its
// detached ".asc" signature when the index has one, into one
// application/x-tgz archive. Nothing is verified; signatures are stored exactly
// as served. The archive shape is the same for one file and for many, so
// consumers never have to branch on the entry count.
//
// Each response body is streamed into the archive, so the archive is the only
// copy held in memory.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	p, err := internal.ConvertAccess(resource)
	if err != nil {
		return nil, err
	}
	creds, err := credsv1.ConvertToPyPICredentials(credentials)
	if err != nil {
		return nil, err
	}
	refs, err := r.client.Resolve(ctx, p, creds)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	archive, err := newArchiveWriter(&buf)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		resp, _, err := r.fetch(ctx, ref.URL, creds, false)
		if err != nil {
			return nil, err
		}
		if err := archive.addResponse(ref.Filename, resp); err != nil {
			return nil, err
		}
		// The index either advertises the signature (HasGPGSig) or does not say;
		// in both cases probe it optionally, so a repository that serves an
		// unadvertised signature is still mirrored, and a missing one is not an
		// error.
		resp, found, err := r.fetch(ctx, ref.URL+pypi.GPGSignatureSuffix, creds, true)
		if err != nil {
			return nil, err
		}
		if found {
			if err := archive.addResponse(ref.Filename+pypi.GPGSignatureSuffix, resp); err != nil {
				return nil, err
			}
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return inmemory.New(bytes.NewReader(buf.Bytes()), inmemory.WithMediaType(MediaTypeTGZ)), nil
}

// fetch GETs url and returns the response when the status is 200. A 404 is
// reported as found=false only for an optional file (a signature): not every
// index signs every file, but a listed distribution file must exist. Any other
// status is an error, so an index that refuses to serve a signature it has
// cannot silently produce an incomplete archive.
func (r *ResourceRepository) fetch(ctx context.Context, url string, credentials *credsv1.PyPICredentials, optional bool) (resp *http.Response, found bool, err error) {
	kind := "pypi distribution"
	if optional {
		kind = "pypi signature"
	}
	resp, err = r.client.Get(ctx, url, "", credentials)
	if err != nil {
		return nil, false, fmt.Errorf("error downloading %s %q: %w", kind, url, err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, true, nil
	}
	_ = resp.Body.Close()
	if optional && resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("error downloading %s %q: unexpected status %d", kind, url, resp.StatusCode)
}

// archiveWriter writes a flat gzip-compressed tar.
type archiveWriter struct {
	gz *gzip.Writer
	tw *tar.Writer
}

// newArchiveWriter starts an archive on w. The gzip stream uses stored
// (uncompressed) blocks: wheels and sdists are already compressed, and a stored
// block has exactly one encoding, so the archive bytes, and any digest over
// them, do not depend on the compress/flate version that built them. The tar
// headers carry no timestamps or owners for the same reason.
func newArchiveWriter(w io.Writer) (*archiveWriter, error) {
	gz, err := gzip.NewWriterLevel(w, gzip.NoCompression)
	if err != nil {
		return nil, fmt.Errorf("error creating gzip writer: %w", err)
	}
	return &archiveWriter{gz: gz, tw: tar.NewWriter(gz)}, nil
}

// addResponse writes the body of resp as entry name and closes it. A response
// that does not announce its length is read into memory first, because a tar
// header needs the size before the content.
func (a *archiveWriter) addResponse(name string, resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	var data io.Reader = resp.Body
	size := resp.ContentLength
	if size < 0 {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("error reading %q: %w", name, err)
		}
		data, size = bytes.NewReader(b), int64(len(b))
	}
	return a.add(name, size, data)
}

func (a *archiveWriter) add(name string, size int64, data io.Reader) error {
	if err := a.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: size}); err != nil {
		return fmt.Errorf("error writing tar header for %q: %w", name, err)
	}
	if _, err := io.Copy(a.tw, data); err != nil {
		return fmt.Errorf("error writing tar entry %q: %w", name, err)
	}
	return nil
}

func (a *archiveWriter) Close() error {
	if err := a.tw.Close(); err != nil {
		return fmt.Errorf("error closing tar: %w", err)
	}
	if err := a.gz.Close(); err != nil {
		return fmt.Errorf("error closing gzip: %w", err)
	}
	return nil
}

// UploadResource deploys the files of a download archive to a writable PyPI
// index. content must be the application/x-tgz that DownloadResource produced:
// the selected distribution files, each optionally followed by its ".asc"
// signature. Every entry is written under the project path exactly as it is in
// the archive; no hashes are computed or checked.
//
// The archive is checked against the spec before the first PUT, so an archive
// that misses a resolved file or holds an unlisted entry is refused whole.
// Entries are then written one by one. A failing PUT stops the upload and
// leaves the entries before it deployed: an index has no transaction to roll
// back.
func (r *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	p, err := internal.ConvertAccess(resource)
	if err != nil {
		return nil, err
	}
	creds, err := credsv1.ConvertToPyPICredentials(credentials)
	if err != nil {
		return nil, err
	}

	names, err := listArchive(content)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("upload archive holds no files")
	}
	if err := checkArchiveMatchesSpec(names); err != nil {
		return nil, err
	}

	base, err := url.Parse(p.IndexURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing indexUrl %q: %w", p.IndexURL, err)
	}
	project := v1alpha1.NormalizeProjectName(p.Project)

	err = walkArchive(content, func(h *tar.Header, data io.Reader) error {
		fileURL := base.JoinPath(project, h.Name).String()
		return r.client.Put(ctx, fileURL, data, h.Size, mediaTypeFor(h.Name), creds)
	})
	if err != nil {
		return nil, err
	}
	return resource.DeepCopy(), nil
}

// mediaTypeFor returns the Content-Type to send when uploading a file, keyed
// off its extension. A fixed table keeps the header the same on every machine.
func mediaTypeFor(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".whl"):
		return "application/octet-stream"
	case strings.HasSuffix(filename, ".tar.gz"):
		return "application/gzip"
	case strings.HasSuffix(filename, ".zip"):
		return "application/zip"
	case strings.HasSuffix(filename, pypi.GPGSignatureSuffix):
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// listArchive returns the entry names of content in archive order without
// keeping any entry content, so the archive can be checked against the spec
// before the first PUT.
func listArchive(content blob.ReadOnlyBlob) ([]string, error) {
	var names []string
	err := walkArchive(content, func(h *tar.Header, _ io.Reader) error {
		names = append(names, h.Name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// walkArchive opens content as a flat gzip-compressed tar and calls fn for
// every entry with a reader over its content. Only regular files are accepted;
// a directory or link entry could not have come from a download.
func walkArchive(content blob.ReadOnlyBlob, fn func(h *tar.Header, data io.Reader) error) error {
	rc, err := content.ReadCloser()
	if err != nil {
		return fmt.Errorf("error reading upload content: %w", err)
	}
	defer func() { _ = rc.Close() }()

	gz, err := gzip.NewReader(rc)
	if err != nil {
		return fmt.Errorf("upload content is not an %s archive: %w", MediaTypeTGZ, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("error reading upload archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("upload archive entry %q is not a regular file", h.Name)
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

// checkArchiveMatchesSpec rejects an archive that holds a path-unsafe entry, a
// duplicate entry, or a signature without its file. Unlike Maven, PyPI does not
// pre-compute file names from coordinates (they carry platform tags), so the
// archive's own distribution file names are trusted as the set to deploy; the
// checks guard only against a crafted archive escaping the project path or
// deploying the same file twice.
func checkArchiveMatchesSpec(names []string) error {
	files := make(map[string]struct{}, len(names))
	seen := make(map[string]struct{}, len(names))
	var errs []error
	for _, name := range names {
		if name != path.Base(name) || strings.Contains(name, "..") {
			errs = append(errs, fmt.Errorf("upload archive entry %q is not a plain file name", name))
			continue
		}
		if _, dup := seen[name]; dup {
			errs = append(errs, fmt.Errorf("upload archive entry %q appears more than once", name))
			continue
		}
		seen[name] = struct{}{}
		if !strings.HasSuffix(name, pypi.GPGSignatureSuffix) {
			files[name] = struct{}{}
		}
	}
	for name := range seen {
		if strings.HasSuffix(name, pypi.GPGSignatureSuffix) {
			if _, ok := files[strings.TrimSuffix(name, pypi.GPGSignatureSuffix)]; !ok {
				errs = append(errs, fmt.Errorf("upload archive holds signature %q without its file", name))
			}
		}
	}
	return errors.Join(errs...)
}
