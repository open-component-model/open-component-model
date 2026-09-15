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
	"path"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/internal"
	"ocm.software/open-component-model/bindings/go/maven/internal/maven"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	credsv1 "ocm.software/open-component-model/bindings/go/maven/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MediaTypeTGZ is the media type of every download.
const MediaTypeTGZ = "application/x-tgz"

// ResourceRepository downloads and uploads Maven artifacts over HTTP(S).
type ResourceRepository struct {
	client *maven.Client
}

// Option configures a ResourceRepository.
type Option func(*ResourceRepository)

// WithHTTPClient sets the HTTP client used for download and upload. Defaults
// to http.DefaultClient; the CLI passes its configured client so timeouts,
// retries and TLS settings apply.
func WithHTTPClient(c *http.Client) Option {
	return func(r *ResourceRepository) { r.client = maven.NewClient(c) }
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a Maven ResourceRepository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	r := &ResourceRepository{}
	for _, opt := range opts {
		opt(r)
	}
	if r.client == nil {
		r.client = maven.NewClient(nil)
	}
	return r
}

// GetResourceRepositoryScheme returns the Maven access scheme.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return mavenaccess.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the "MavenRepository" identity for the resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	m, err := internal.ConvertAccess(resource)
	if err != nil {
		return nil, err
	}
	return internal.CredentialConsumerIdentity(m.RepoURL)
}

// DownloadResource fetches every file listed in the access spec together with
// the sibling files the repository publishes next to it (the ".asc"
// signature and the checksum files, see maven.SiblingSuffixes), and packages
// them into one application/x-tgz archive: each file followed by its
// siblings. Nothing is verified; siblings are stored exactly as served. The
// archive shape is the same for one file and for many, so consumers never
// have to branch on the entry count.
//
// Each response body is streamed into the archive, so the archive is the only
// copy held in memory.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	m, err := internal.ConvertAccess(resource)
	if err != nil {
		return nil, err
	}
	creds, err := credsv1.ConvertToMavenCredentials(credentials)
	if err != nil {
		return nil, err
	}
	refs, err := r.client.Resolve(ctx, m, creds)
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
		for _, suffix := range maven.SiblingSuffixes {
			resp, found, err := r.fetch(ctx, ref.URL+suffix, creds, true)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			if err := archive.addResponse(ref.Filename+suffix, resp); err != nil {
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
// reported as found=false only for an optional file: not every repository
// signs or publishes every checksum, but a listed file must exist. Any other
// status is an error, so a repository that refuses to serve a sibling it has
// cannot silently produce an incomplete archive.
func (r *ResourceRepository) fetch(ctx context.Context, url string, credentials *credsv1.MavenCredentials, optional bool) (resp *http.Response, found bool, err error) {
	kind := "maven artifact"
	if optional {
		kind = "maven sibling"
	}
	resp, err = r.client.Get(ctx, url, credentials)
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
// (uncompressed) blocks: the listed files are mostly jars, which are already
// compressed, and a stored block has exactly one encoding, so the archive
// bytes, and any digest over them, do not depend on the compress/flate
// version that built them. The tar headers carry no timestamps or owners for
// the same reason.
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

// UploadResource deploys the files of a download archive to a Maven
// repository. content must be the application/x-tgz that DownloadResource
// produced: every file listed in the access spec, each optionally followed by
// its sibling files. Every entry is written to the release path exactly as it
// is in the archive; no checksums are computed or checked.
//
// The archive is checked against the spec before the first PUT, so an archive
// that misses a listed file or holds an unlisted entry is refused whole.
// Entries are then written one by one. A failing PUT stops the upload and
// leaves the entries before it deployed: a Maven repository has no
// transaction to roll back. SNAPSHOT versions are rejected, since deploying
// one also needs the version-level maven-metadata.xml rewritten, and so are
// LATEST and RELEASE, which name no version directory to deploy into.
func (r *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	m, err := internal.ConvertAccess(resource)
	if err != nil {
		return nil, err
	}
	if maven.IsSnapshot(m.Version) {
		return nil, fmt.Errorf("upload of SNAPSHOT version %q is not supported: it needs maven-metadata.xml to be rewritten", m.Version)
	}
	if !m.IsPinnedVersion() {
		return nil, fmt.Errorf("upload of version %q is not supported: LATEST and RELEASE name no version directory, deploy to a concrete version", m.Version)
	}
	creds, err := credsv1.ConvertToMavenCredentials(credentials)
	if err != nil {
		return nil, err
	}

	names, err := listArchive(content)
	if err != nil {
		return nil, err
	}
	if err := checkArchiveMatchesSpec(m, names); err != nil {
		return nil, err
	}

	err = walkArchive(content, func(h *tar.Header, data io.Reader) error {
		fileURL, err := maven.FileURL(m, h.Name)
		if err != nil {
			return err
		}
		contentType := maven.MediaTypeFor(strings.TrimPrefix(path.Ext(h.Name), "."))
		return r.client.Put(ctx, fileURL, data, h.Size, contentType, creds)
	})
	if err != nil {
		return nil, err
	}
	return resource.DeepCopy(), nil
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
// every entry with a reader over its content. Only regular files are
// accepted; a directory or link entry could not have come from a download.
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

// checkArchiveMatchesSpec rejects an archive that does not hold exactly the
// files the spec lists, plus optional siblings for them. Entry names are
// matched against names computed from the spec, so a crafted archive cannot
// choose its own upload path. A listed name is matched before it is read as a
// sibling, so an artifact whose extension is itself a sibling suffix (an
// "asc" entry) still uploads.
func checkArchiveMatchesSpec(m *v2alpha1.Maven, names []string) error {
	listed := make(map[string]bool, len(m.Artifacts))
	for _, a := range m.Artifacts {
		listed[maven.ReleaseFileName(m, a)] = false
	}
	var errs []error
	for _, name := range names {
		if _, ok := listed[name]; ok {
			listed[name] = true
			continue
		}
		file, suffix := maven.SplitSibling(name)
		if _, ok := listed[file]; !ok || suffix == "" {
			errs = append(errs, fmt.Errorf("upload archive entry %q is not listed in the access spec", name))
		}
	}
	for name, seen := range listed {
		if !seen {
			errs = append(errs, fmt.Errorf("upload archive is missing %q listed in the access spec", name))
		}
	}
	return errors.Join(errs...)
}
