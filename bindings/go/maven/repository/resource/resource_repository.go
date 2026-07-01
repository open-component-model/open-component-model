package resource

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/internal"
	coordinates "ocm.software/open-component-model/bindings/go/maven/internal/maven"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository downloads and uploads Maven artifacts over HTTP(S).
type ResourceRepository struct {
	client *http.Client
}

// Option configures a ResourceRepository.
type Option func(*ResourceRepository)

// WithHTTPClient sets the HTTP client used for download/upload. Defaults to http.DefaultClient.
func WithHTTPClient(c *http.Client) Option {
	return func(r *ResourceRepository) { r.client = c }
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a Maven ResourceRepository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	r := &ResourceRepository{client: http.DefaultClient}
	for _, opt := range opts {
		opt(r)
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

// DownloadResource downloads the Maven artifact described by resource and returns it as an in-memory blob.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	m, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}
	artifactURL, err := coordinates.ArtifactURL(m)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request for %q: %w", artifactURL, err)
	}
	if err := r.applyCredentials(req, credentials); err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error downloading maven artifact %q: %w", artifactURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error downloading maven artifact %q: unexpected status %d", artifactURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading maven artifact body: %w", err)
	}
	mediaType := m.MediaType
	if mediaType == "" {
		mediaType = coordinates.DefaultMediaType(m)
	}
	return inmemory.New(bytes.NewReader(data), inmemory.WithMediaType(mediaType)), nil
}

// UploadResource uploads the Maven artifact and its sha1/md5 checksums, returning an updated resource.
func (r *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	m, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}
	artifactURL, err := coordinates.ArtifactURL(m)
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

	mediaType := m.MediaType
	if mediaType == "" {
		mediaType = coordinates.DefaultMediaType(m)
	}

	if err := r.put(ctx, artifactURL, data, mediaType, credentials); err != nil {
		return nil, err
	}
	sha1sum := fmt.Sprintf("%x", sha1.Sum(data))
	if err := r.put(ctx, artifactURL+".sha1", []byte(sha1sum), "text/plain", credentials); err != nil {
		return nil, err
	}
	md5sum := fmt.Sprintf("%x", md5.Sum(data))
	if err := r.put(ctx, artifactURL+".md5", []byte(md5sum), "text/plain", credentials); err != nil {
		return nil, err
	}

	updated := resource.DeepCopy()
	uploaded := m.DeepCopy()
	uploaded.MediaType = mediaType
	updated.Access = uploaded
	return updated, nil
}

func (r *ResourceRepository) put(ctx context.Context, url string, data []byte, contentType string, credentials runtime.Typed) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("error creating PUT request for %q: %w", url, err)
	}
	req.Header.Set("Content-Type", contentType)
	if err := r.applyCredentials(req, credentials); err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("error uploading to %q: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("error uploading to %q: unexpected status %d", url, resp.StatusCode)
	}
	return nil
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

// credentialScheme converts runtime.Typed credentials (incl. DirectCredentials).
var credentialScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	credv1.MustRegister(s)
	return s
}()

// applyCredentials sets Authorization headers on req from runtime.Typed credentials.
// A non-empty accessToken yields Bearer auth; otherwise username/password yields Basic auth.
func (r *ResourceRepository) applyCredentials(req *http.Request, credentials runtime.Typed) error {
	if credentials == nil || credentials.GetType().String() == "" {
		return nil
	}
	typed, err := credentialScheme.NewObject(credentials.GetType())
	if err != nil {
		return fmt.Errorf("error resolving credential type: %w", err)
	}
	if err := credentialScheme.Convert(credentials, typed); err != nil {
		return fmt.Errorf("error converting credentials: %w", err)
	}
	direct, ok := typed.(*credv1.DirectCredentials)
	if !ok {
		return fmt.Errorf("unsupported credential type for maven: %v", typed.GetType())
	}
	props := direct.Properties
	if token := props["accessToken"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	if username := props["username"]; username != "" {
		req.SetBasicAuth(username, props["password"])
		return nil
	}
	return fmt.Errorf("maven credentials present but neither accessToken nor username is set")
}
