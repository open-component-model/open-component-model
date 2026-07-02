package resource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/internal"
	"ocm.software/open-component-model/bindings/go/maven/internal/maven"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository downloads and uploads Maven artifacts over HTTP(S).
type ResourceRepository struct {
	client       *maven.Client
	hardVerify   bool
	verifyUpload bool
}

// Option configures a ResourceRepository.
type Option func(*ResourceRepository)

// WithHTTPClient sets the HTTP client used for download/upload. Defaults to http.DefaultClient.
func WithHTTPClient(c *http.Client) Option {
	return func(r *ResourceRepository) { r.client = maven.NewClient(c) }
}

// WithHardVerify makes DownloadResource fail when no ".sha1" checksum sibling is
// available. By default a missing checksum is tolerated (soft verification).
func WithHardVerify() Option {
	return func(r *ResourceRepository) { r.hardVerify = true }
}

// WithVerifyUpload re-downloads the artifact after upload and fails if its bytes
// do not match what was uploaded. Off by default (a 2xx response is trusted).
func WithVerifyUpload() Option {
	return func(r *ResourceRepository) { r.verifyUpload = true }
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
	m, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}
	return internal.CredentialConsumerIdentity(m.RepoURL)
}

// DownloadResource resolves the Maven artifact(s) described by resource and
// returns them as a single blob, or an application/x-tgz when the (SNAPSHOT)
// selector resolves to more than one file.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	m, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}
	refs, err := r.client.Resolve(ctx, m, credentials)
	if err != nil {
		return nil, err
	}
	if len(refs) == 1 {
		data, err := r.fetchFile(ctx, refs[0], credentials)
		if err != nil {
			return nil, err
		}
		return inmemory.New(bytes.NewReader(data), inmemory.WithMediaType(refs[0].MediaType)), nil
	}

	// NOTE: the tgz produced below is built locally with archive/tar and
	// compress/gzip. Its exact bytes (and therefore any digest computed over
	// them) are not guaranteed to be reproducible across Go releases, since
	// compress/flate's output can change between versions. Digests over
	// multi-file (tgz) selections are therefore best-effort, not a stable
	// content-addressable identity.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, ref := range refs {
		data, err := r.fetchFile(ctx, ref, credentials)
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: ref.Filename, Mode: 0o644, Size: int64(len(data))}); err != nil {
			return nil, fmt.Errorf("error writing tar header for %q: %w", ref.Filename, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("error writing tar entry %q: %w", ref.Filename, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("error closing tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("error closing gzip: %w", err)
	}
	return inmemory.New(bytes.NewReader(buf.Bytes()), inmemory.WithMediaType("application/x-tgz")), nil
}

// fetchFile GETs one resolved file and verifies its .sha1 sibling.
func (r *ResourceRepository) fetchFile(ctx context.Context, ref maven.FileRef, credentials runtime.Typed) ([]byte, error) {
	data, status, err := r.client.Get(ctx, ref.URL, credentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading maven artifact %q: %w", ref.URL, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("error downloading maven artifact %q: unexpected status %d", ref.URL, status)
	}
	if err := r.verifyChecksum(ctx, ref.URL, data, credentials); err != nil {
		return nil, err
	}
	return data, nil
}

// verifyChecksum fetches the ".sha1" sibling of artifactURL and verifies data
// against it. A missing sibling (404) or empty body is a soft pass unless
// WithHardVerify is set; any other non-200 status is a hard error.
func (r *ResourceRepository) verifyChecksum(ctx context.Context, artifactURL string, data []byte, credentials runtime.Typed) error {
	checksumURL := artifactURL + maven.ChecksumExtension
	body, status, err := r.client.Get(ctx, checksumURL, credentials)
	if err != nil {
		return fmt.Errorf("error fetching checksum %q: %w", checksumURL, err)
	}
	if status == http.StatusNotFound {
		// Genuinely absent checksum: tolerated in soft mode.
		if r.hardVerify {
			return fmt.Errorf("no checksum available for %q and hard verification is enabled", checksumURL)
		}
		return nil
	}
	if status != http.StatusOK {
		// Any other non-2xx (401/403/5xx) is a fetch failure, not a missing
		// checksum — never silently skip integrity verification for it.
		return fmt.Errorf("error fetching checksum %q: unexpected status %d", checksumURL, status)
	}
	expected := maven.ParseSHA1File(body)
	if expected == "" {
		if r.hardVerify {
			return fmt.Errorf("empty checksum file %q and hard verification is enabled", checksumURL)
		}
		return nil
	}
	if err := maven.VerifySHA1(data, expected); err != nil {
		return fmt.Errorf("integrity check failed for %q: %w", artifactURL, err)
	}
	return nil
}

// UploadResource uploads the Maven artifact and its sha1/md5 checksums, returning an updated resource.
func (r *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	m, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}
	artifactURL, err := maven.ArtifactURL(m)
	if err != nil {
		return nil, err
	}

	rc, err := content.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error reading artifact content: %w", err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return nil, fmt.Errorf("error reading artifact content: %w", err)
	}

	mediaType := maven.UploadMediaType(m)

	if err := r.client.Put(ctx, artifactURL, data, mediaType, credentials); err != nil {
		return nil, err
	}
	sha1sum := fmt.Sprintf("%x", sha1.Sum(data))
	if err := r.client.Put(ctx, artifactURL+".sha1", []byte(sha1sum), "text/plain", credentials); err != nil {
		return nil, err
	}
	md5sum := fmt.Sprintf("%x", md5.Sum(data))
	if err := r.client.Put(ctx, artifactURL+".md5", []byte(md5sum), "text/plain", credentials); err != nil {
		return nil, err
	}
	sha256sum := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := r.client.Put(ctx, artifactURL+".sha256", []byte(sha256sum), "text/plain", credentials); err != nil {
		return nil, err
	}

	if r.verifyUpload {
		got, status, err := r.client.Get(ctx, artifactURL, credentials)
		if err != nil {
			return nil, fmt.Errorf("error verifying upload %q: %w", artifactURL, err)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("error verifying upload %q: unexpected status %d", artifactURL, status)
		}
		if err := maven.VerifySHA1(got, maven.SHA1Hex(data)); err != nil {
			return nil, fmt.Errorf("upload verification failed for %q: %w", artifactURL, err)
		}
	}

	updated := resource.DeepCopy()
	uploaded := m.DeepCopy()
	uploaded.MediaType = &mediaType
	updated.Access = uploaded
	return updated, nil
}

func (r *ResourceRepository) convertAccess(resource *descriptor.Resource) (*v1.Maven, error) {
	if resource == nil || resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	var m v1.Maven
	if err := mavenaccess.Scheme.Convert(resource.Access, &m); err != nil {
		return nil, fmt.Errorf("error converting access to maven spec: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid maven access: %w", err)
	}
	return &m, nil
}
