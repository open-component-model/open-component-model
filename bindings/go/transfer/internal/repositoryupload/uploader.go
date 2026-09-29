// Package repositoryupload holds what the Artifactory and Nexus uploaders share: the
// transformation spec, the source and target plumbing, and the Helm chart upload flow.
package repositoryupload

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/chartarchive"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

const (
	// hashAlgorithmSHA256 is the hash algorithm recorded for uploaded content digests.
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain streamed blob.
	genericBlobDigestV1 = "genericBlobDigest/v1"
	// MaxErrorBodyBytes bounds how much of a non-2xx response body is read into an error.
	MaxErrorBodyBytes = 4 << 10
	// PollAttempts bounds how often the chart metadata a server records for an
	// uploaded chart is polled before the content is considered not to be a helm chart. The
	// metadata can lag behind the upload: Artifactory may calculate the chart.name and
	// chart.version properties asynchronously, Nexus indexes components for search about 2 s
	// after the upload (Nexus 3.96). 30 polls allow about 15 s for a server under load.
	PollAttempts = 30
	// DefaultPollInterval is the wait between two chart metadata polls.
	DefaultPollInterval = 500 * time.Millisecond
	// octetStream is the content type of uploaded content of unknown media type.
	octetStream = "application/octet-stream"
)

// Spec is the input specification of an Artifactory or Nexus upload
// transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type Spec struct {
	// Resource is the source resource to upload.
	Resource *descriptorv2.Resource `json:"resource"`
	// ComponentVersion is the component version holding the resource. It determines the default
	// upload location and, for local blob resources, where the resource is read from.
	ComponentVersion *ComponentVersion `json:"componentVersion"`
	// URL is the base URL of the server.
	URL string `json:"url"`
	// Repository is the name of the target repository.
	Repository string `json:"repository"`
	// Path is where the content is stored, relative to the repository root. It must consist of
	// non-empty segments without . or .. and, for helm repositories, end in .tgz. Empty stores the
	// content under <component>/<component version>/<resource>-<resource version>.
	Path string `json:"path,omitempty"`
}

// ComponentVersion identifies the component version holding the resource.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ComponentVersion struct {
	// Repository is the specification of the repository holding the component version. It is
	// set for local blob resources only, which are read from it.
	Repository *runtime.Raw `json:"repository,omitempty"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
}

// Output is the output of an Artifactory or Nexus upload transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type Output struct {
	// Resource is the uploaded resource with its access on the target repository.
	Resource *descriptorv2.Resource `json:"resource"`
}

// Uploader holds what every repository upload needs: it opens the source resource
// and talks to the target server. The content is streamed and never buffered on disk.
type Uploader struct {
	Scheme *runtime.Scheme
	Charts *chartarchive.Source
	// ResourceRepository derives the source credential identities of remote resources.
	ResourceRepository repository.ResourceRepository
	// RepoProvider resolves the source repositories of local blob resources.
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
	HTTPConfig         *httpv1alpha1.Config

	// PollInterval is the wait between two metadata or search polls; zero uses DefaultPollInterval.
	PollInterval time.Duration
}

// ValidateSpec rejects a spec missing a field every upload needs.
func ValidateSpec(spec *Spec) error {
	switch {
	case spec == nil:
		return fmt.Errorf("spec is required")
	case spec.Resource == nil:
		return fmt.Errorf("source resource is required")
	case spec.ComponentVersion == nil || spec.ComponentVersion.Component == "" || spec.ComponentVersion.Version == "":
		return fmt.Errorf("component and version are required")
	case spec.URL == "":
		return fmt.Errorf("url is required")
	case spec.Repository == "":
		return fmt.Errorf("repository is required")
	}
	return nil
}

// Target resolves the upload credentials, see resolveTargetCredentials, and returns a client
// sending requests with them.
func (u *Uploader) Target(ctx context.Context, helmRepo, repoURL string) (*Client, error) {
	creds, err := u.resolveTargetCredentials(ctx, helmRepo, repoURL)
	if err != nil {
		return nil, err
	}
	return &Client{httpConfig: u.HTTPConfig, creds: creds}, nil
}

// Open returns the request opening the source resource: from the source component version for
// local blobs, else with the resolved source credentials.
func (u *Uploader) Open(ctx context.Context, spec *Spec, src *descriptor.Resource) (chartarchive.Request, error) {
	req := chartarchive.Request{Resource: src}
	var err error
	if spec.ComponentVersion.Repository != nil {
		req.Local, err = u.localSource(ctx, spec.ComponentVersion)
	} else {
		req.Credentials, err = u.resolveSourceCredentials(ctx, src)
	}
	return req, err
}

// Interval is the wait between two metadata or search polls.
func (u *Uploader) Interval() time.Duration {
	if u.PollInterval == 0 {
		return DefaultPollInterval
	}
	return u.PollInterval
}

// Output converts the uploaded resource to its v2 form.
func (u *Uploader) Output(out *descriptor.Resource) (*Output, error) {
	res, err := descriptor.ConvertToV2Resource(u.Scheme, out)
	if err != nil {
		return nil, fmt.Errorf("failed converting uploaded resource to v2 format: %w", err)
	}
	return &Output{Resource: res}, nil
}

// KnownDigest returns the SHA-256 the uploaded content must have (expected, see expectedDigest)
// and the one it is known to have up front (known): expected, else the digest the content
// reports itself.
func KnownDigest(src *descriptor.Digest, content blob.ReadOnlyBlob, fromOCI bool) (expected, known string, err error) {
	if expected, err = expectedDigest(src, fromOCI); err != nil {
		return "", "", err
	}
	if expected != "" {
		return expected, expected, nil
	}
	if da, ok := content.(blob.DigestAware); ok {
		if d, ok := da.Digest(); ok {
			if hex, ok := strings.CutPrefix(d, "sha256:"); ok {
				return "", hex, nil
			}
		}
	}
	return "", "", nil
}

// UploadedDigest is the digest of the published resource: the source digest if it describes the
// uploaded content, else the SHA-256 of the uploaded bytes.
func UploadedDigest(src *descriptor.Digest, expected, sha256Hex string) *descriptor.Digest {
	if expected != "" {
		return src.DeepCopy()
	}
	return &descriptor.Digest{HashAlgorithm: hashAlgorithmSHA256, NormalisationAlgorithm: genericBlobDigestV1, Value: sha256Hex}
}

// ContentType is the media type content is uploaded with: that of the content, else that of the
// resource access, else application/octet-stream.
func ContentType(content *chartarchive.Content, res *descriptorv2.Resource) string {
	if content.MediaType != "" {
		return content.MediaType
	}
	if mt := MediaTypeFromAccess(*res); mt != "" {
		return mt
	}
	return octetStream
}

// ResourceFile returns the path-escaped file name of the resource:
// <resource name>-<resource version><ext>, with a hash of the extra identity appended when the
// resource has one, so every resource of a component version has its own file.
func ResourceFile(res *descriptor.Resource, ext string) (string, error) {
	if strings.ContainsAny(res.Name+res.Version, "/\\") {
		return "", fmt.Errorf("resource name %q and version %q must not contain path separators", res.Name, res.Version)
	}
	file := res.Name + "-" + res.Version
	if len(res.ExtraIdentity) > 0 {
		file += fmt.Sprintf("-%016x", res.ExtraIdentity.CanonicalHashV1())
	}
	return url.PathEscape(file + ext), nil
}

// defaultPath returns the path-escaped default location of the resource in the repository:
// <component>/<component version>/<resource file>.
func defaultPath(component, version string, res *descriptor.Resource, ext string) (string, error) {
	file, err := ResourceFile(res, ext)
	if err != nil {
		return "", err
	}
	segments := append(strings.Split(component, "/"), version)
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "\\") {
			return "", fmt.Errorf("component %q version %q cannot be used as a repository path", component, version)
		}
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(append(segments, file), "/"), nil
}

// UploadPath returns the configured location, see CustomPath, else the default location.
func UploadPath(spec *Spec, res *descriptor.Resource, ext string) (string, error) {
	if spec.Path != "" {
		return CustomPath(spec.Path, ext)
	}
	return defaultPath(spec.ComponentVersion.Component, spec.ComponentVersion.Version, res, ext)
}

// CustomPath validates a configured location and returns it path-escaped. It must be relative,
// consist of non-empty segments other than . and .., and end in requiredSuffix, so it can
// neither leave the repository nor address a folder.
func CustomPath(path, requiredSuffix string) (string, error) {
	if !strings.HasSuffix(path, requiredSuffix) {
		return "", fmt.Errorf("path %q must end in %s", path, requiredSuffix)
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "\\") {
			return "", fmt.Errorf("path %q must be relative without empty, \".\" or \"..\" segments", path)
		}
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/"), nil
}

// expectedDigest returns the SHA-256 the uploaded chart must have: the source digest, if it
// describes the uploaded bytes. It is empty when there is no source digest or the chart was
// extracted from an OCI artifact, whose digest describes a different byte representation.
func expectedDigest(src *descriptor.Digest, fromOCI bool) (string, error) {
	if src == nil || fromOCI {
		return "", nil
	}
	if src.HashAlgorithm != hashAlgorithmSHA256 {
		return "", fmt.Errorf("unsupported hash algorithm: expected %s, got %s", hashAlgorithmSHA256, src.HashAlgorithm)
	}
	if src.NormalisationAlgorithm != genericBlobDigestV1 {
		return "", fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, src.NormalisationAlgorithm)
	}
	return src.Value, nil
}

// localSource resolves the source component version repository of a local blob resource.
func (t *Uploader) localSource(ctx context.Context, cv *ComponentVersion) (*chartarchive.Local, error) {
	if t.RepoProvider == nil {
		return nil, fmt.Errorf("no component version repository provider configured for local resources")
	}
	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, cv.Repository); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving source repository credentials: %w", err)
			}
		}
	}
	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, cv.Repository, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting source component version repository: %w", err)
	}
	return &chartarchive.Local{Repository: repo, Component: cv.Component, Version: cv.Version}, nil
}

// resolveSourceCredentials resolves credentials for a remote source resource by its consumer
// identity. A missing provider or ErrNotFound yields nil credentials.
func (t *Uploader) resolveSourceCredentials(ctx context.Context, resource *descriptor.Resource) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	consumerID, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("failed deriving source consumer identity: %w", err)
	}
	if consumerID == nil {
		return nil, nil
	}
	creds, err := t.CredentialProvider.Resolve(ctx, consumerID)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed resolving source credentials: %w", err)
	}
	return creds, nil
}

// resolveTargetCredentials resolves the upload credentials: those of the HelmChartRepository
// identity of the Helm repository URL of the target repository, falling back to the Wget
// identity of its repository URL. Without either, the upload is anonymous. HelmHTTPCredentials are mapped to their
// username and password, which is all an HTTP upload uses.
func (t *Uploader) resolveTargetCredentials(ctx context.Context, helmRepo, repoURL string) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	helmID, err := runtime.ParseURLToIdentity(helmRepo)
	if err != nil {
		return nil, fmt.Errorf("failed deriving target consumer identity: %w", err)
	}
	helmID.SetType(helmidentityv1.Type)
	wgetID, err := wgetidentityv1.IdentityFromURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed deriving target consumer identity: %w", err)
	}

	var creds runtime.Typed
	for _, id := range []runtime.Identity{helmID, wgetID} {
		creds, err = t.CredentialProvider.Resolve(ctx, id)
		if err == nil {
			break
		}
		if !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("failed resolving target credentials: %w", err)
		}
		creds = nil
	}
	if creds == nil || creds.GetType().Name != helmcredsv1.HelmHTTPCredentialsType {
		return creds, nil
	}
	helmCreds, err := helmcredsv1.ConvertToHelmHTTPCredentials(creds)
	if err != nil {
		return nil, fmt.Errorf("failed converting target credentials: %w", err)
	}
	if helmCreds.CertFile != "" || helmCreds.KeyFile != "" {
		return nil, fmt.Errorf("HelmHTTPCredentials certFile/keyFile are not supported for repository uploads; use WgetCredentials/v1 certificate and privateKey")
	}
	return &wgetcredsv1.WgetCredentials{
		Type:     wgetcredsv1.WgetCredentialsVersionedType,
		Username: helmCreds.Username,
		Password: helmCreds.Password,
	}, nil
}

// UploadBlob streams content to putURL and returns the hex SHA-256 of the bytes read and
// whether content was read to its end. A successful response body is decoded into out, if set.
func UploadBlob(ctx context.Context, c *Client, content blob.ReadOnlyBlob, putURL string, header http.Header, out any) (sha256Hex string, complete bool, err error) {
	rc, err := content.ReadCloser()
	if err != nil {
		return "", false, fmt.Errorf("failed opening content: %w", err)
	}
	defer func() { _ = rc.Close() }()
	size := blob.SizeUnknown
	if sized, ok := content.(blob.SizeAware); ok {
		size = sized.Size()
	}
	hasher := sha256.New()
	body := &eofReader{r: rc}
	err = c.Send(ctx, http.MethodPut, putURL, io.TeeReader(body, hasher), size, header, out)
	return godigest.NewDigestFromBytes(godigest.SHA256, hasher.Sum(nil)).Encoded(), body.eof, err
}

// eofReader records whether its reader returned io.EOF, i.e. was read to its end.
type eofReader struct {
	r   io.Reader
	eof bool
}

func (e *eofReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if errors.Is(err, io.EOF) {
		e.eof = true
	}
	return n, err
}

// Client sends authenticated requests to the repository server.
type Client struct {
	httpConfig *httpv1alpha1.Config
	creds      runtime.Typed
}

// Send issues a single request and fails on a non-2xx response. A successful JSON response is
// decoded into out, if set. Errors never carry userinfo, query or fragment of target.
func (c *Client) Send(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header, out any) error {
	resp, err := c.Do(ctx, method, target, body, size, header)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	safe := RedactURL(target)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorBodyBytes))
		if msg := strings.TrimSpace(string(excerpt)); msg != "" {
			return fmt.Errorf("%s %s returned status %d: %s", method, safe, resp.StatusCode, msg)
		}
		return fmt.Errorf("%s %s returned status %d", method, safe, resp.StatusCode)
	}
	if out != nil {
		if err := DecodeJSONBody(resp.Body, out); err != nil {
			return fmt.Errorf("failed decoding response of %s %s: %w", method, safe, err)
		}
	}
	return nil
}

// DecodeJSONBody decodes a JSON response body into out; an empty body leaves out unchanged.
func DecodeJSONBody(body io.Reader, out any) error {
	if err := json.NewDecoder(io.LimitReader(body, MaxErrorBodyBytes)).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Do sends a single authenticated request. The caller closes the response body.
func (c *Client) Do(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header) (*http.Response, error) {
	safe := RedactURL(target)
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("failed creating %s request for %s: %w", method, safe, err)
	}
	if size >= 0 {
		req.ContentLength = size
	}
	for k, v := range header {
		req.Header[k] = v
	}
	client := ocmhttp.New(ocmhttp.WithConfig(c.httpConfig))
	if err := httpauth.Apply(ctx, req, &client, c.creds); err != nil {
		return nil, fmt.Errorf("failed applying target credentials: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		// client.Do wraps errors in a *url.Error carrying the full request URL.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = safe
		}
		return nil, fmt.Errorf("%s %s failed: %w", method, safe, err)
	}
	return resp, nil
}

// RedactURL strips userinfo, query and fragment so credentials or presigned parameters never
// reach logs or errors.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// MediaTypeFromAccess extracts the source access media type (if any) from the resource
// access, used as the default target media type. Returns "" when absent.
func MediaTypeFromAccess(resource descriptorv2.Resource) string {
	if resource.Access == nil || len(resource.Access.Data) == 0 {
		return ""
	}
	var access struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(resource.Access.Data, &access); err != nil {
		return ""
	}
	return access.MediaType
}
