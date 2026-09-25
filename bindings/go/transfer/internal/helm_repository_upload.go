package internal

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

const (
	HelmRepositoryUploadType    = "HelmRepositoryUpload"
	helmRepositoryUploadVersion = "v1alpha1"

	// hashAlgorithmSHA256 is the hash algorithm recorded for uploaded chart digests.
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain streamed blob.
	genericBlobDigestV1 = "genericBlobDigest/v1"
	// maxErrorBodyBytes bounds how much of a non-2xx response body is read into an error.
	maxErrorBodyBytes = 4 << 10
	// chartMetadataAttempts bounds how often the chart metadata Artifactory records on
	// deployment is polled before the content is considered not to be a helm chart.
	chartMetadataAttempts = 6
	// defaultChartMetadataInterval is the wait between two chart metadata polls.
	defaultChartMetadataInterval = 500 * time.Millisecond
)

// HelmRepositoryUploadVersionedType is the versioned type identifier for HelmRepositoryUpload transformations.
var HelmRepositoryUploadVersionedType = runtime.NewVersionedType(HelmRepositoryUploadType, helmRepositoryUploadVersion)

// HelmRepositoryUploadSpec is the input specification for a HelmRepositoryUpload transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HelmRepositoryUploadSpec struct {
	// Resource is the source resource holding the helm chart.
	Resource *descriptorv2.Resource `json:"resource"`
	// ComponentVersion is the component version holding the resource. It determines where the
	// chart is stored in an Artifactory repository and, for local blob resources, where it is
	// read from.
	ComponentVersion *HelmRepositoryUploadComponentVersion `json:"componentVersion"`
	// Server selects the API of the Helm repository server.
	Server transferv1alpha1.HelmRepositoryServer `json:"server"`
	// URL is the base URL of the Helm repository server.
	URL string `json:"url"`
	// Repository is the name of the Helm repository.
	Repository string `json:"repository"`
	// Reindex requests an index recalculation for the uploaded chart (Artifactory only). A
	// failure is only logged, because Artifactory indexes deployed charts on its own.
	Reindex bool `json:"reindex,omitempty"`
}

// HelmRepositoryUploadComponentVersion identifies the component version holding the resource.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HelmRepositoryUploadComponentVersion struct {
	// Repository is the specification of the repository holding the component version. It is
	// set for local blob resources only, which are read from it.
	Repository *runtime.Raw `json:"repository,omitempty"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
}

// HelmRepositoryUploadOutput is the output of a HelmRepositoryUpload transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HelmRepositoryUploadOutput struct {
	// Resource is the uploaded resource with its Helm/v1 access on the Helm repository.
	Resource *descriptorv2.Resource `json:"resource"`
}

// HelmRepositoryUploadTransformation uploads the packaged Helm chart of a resource to a Helm
// repository of a JFrog Artifactory or Sonatype Nexus server and publishes the resource with a
// Helm/v1 access on it.
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HelmRepositoryUploadTransformation struct {
	// +ocm:jsonschema-gen:enum=HelmRepositoryUpload/v1alpha1
	Type   runtime.Type                `json:"type"`
	ID     string                      `json:"id"`
	Spec   *HelmRepositoryUploadSpec   `json:"spec"`
	Output *HelmRepositoryUploadOutput `json:"output,omitempty"`
}

// HelmRepositoryUpload uploads the packaged chart of a resource to a Helm repository and outputs
// the resource with a Helm/v1 access on it. The chart is streamed and never buffered on disk.
// The chart is not parsed: its name and version are the chart metadata the server records for
// the uploaded chart. Where the chart is stored and how the metadata is read depends on the
// server, see [helmRepositoryServer].
type HelmRepositoryUpload struct {
	Scheme *runtime.Scheme
	Charts *chartarchive.Source
	// ResourceRepository derives the source credential identities of remote resources.
	ResourceRepository repository.ResourceRepository
	// RepoProvider resolves the source repositories of local blob resources.
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
	HTTPConfig         *httpv1alpha1.Config

	// chartMetadataInterval overrides defaultChartMetadataInterval (tests).
	chartMetadataInterval time.Duration
}

func (t *HelmRepositoryUpload) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation HelmRepositoryUploadTransformation
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to HelmRepositoryUpload transformation: %w", err)
	}
	spec := transformation.Spec
	switch {
	case spec == nil:
		return nil, fmt.Errorf("spec is required for HelmRepositoryUpload transformation")
	case spec.Resource == nil:
		return nil, fmt.Errorf("source resource is required")
	case spec.ComponentVersion == nil || spec.ComponentVersion.Component == "" || spec.ComponentVersion.Version == "":
		return nil, fmt.Errorf("component and version are required")
	case spec.Server == "":
		return nil, fmt.Errorf("server is required")
	case spec.URL == "":
		return nil, fmt.Errorf("url is required")
	case spec.Repository == "":
		return nil, fmt.Errorf("repository is required")
	}
	src := descriptor.ConvertFromV2Resource(spec.Resource)
	cv := spec.ComponentVersion
	file, err := chartFile(src)
	if err != nil {
		return nil, err
	}
	path, err := chartPath(cv.Component, cv.Version, src)
	if err != nil {
		return nil, err
	}
	srv, err := t.newServer(spec, path, file)
	if err != nil {
		return nil, err
	}

	req := chartarchive.Request{Resource: src}
	if cv.Repository != nil {
		if req.Local, err = t.localSource(ctx, cv); err != nil {
			return nil, err
		}
	} else if req.Credentials, err = t.resolveSourceCredentials(ctx, src); err != nil {
		return nil, err
	}

	chart, err := t.Charts.Open(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = chart.Close() }()

	creds, err := t.resolveTargetCredentials(ctx, srv.helmRepository(), srv.uploadURL())
	if err != nil {
		return nil, err
	}
	c := &helmClient{httpConfig: t.HTTPConfig, creds: creds}

	expected, err := expectedDigest(src.Digest, chart.FromOCI)
	if err != nil {
		return nil, err
	}
	known := expected
	if known == "" {
		if da, ok := chart.Archive.(blob.DigestAware); ok {
			if d, ok := da.Digest(); ok {
				known, _ = strings.CutPrefix(d, "sha256:")
				if known == d {
					known = ""
				}
			}
		}
	}

	reused := false
	if known != "" {
		if reused, err = srv.reuse(ctx, c, known); err != nil {
			return nil, err
		}
	}
	digestHex := known
	uploadURL := redactURL(srv.uploadURL())
	if reused {
		slog.InfoContext(ctx, "reused helm chart content already stored in the helm repository",
			"server", srv.name(), "resource", src.ToIdentity(), "url", uploadURL)
	} else {
		computed, complete, err := uploadChart(ctx, c, chart, srv.uploadURL(), srv.uploadHeader(known))
		switch {
		case err != nil:
			// A repository that rejects redeploying a chart still stores its content, e.g.
			// from an earlier transfer of the same resource without a source digest.
			if known != "" || !complete || !srv.rejectedUploadStored(ctx, c, computed) {
				return nil, err
			}
			slog.InfoContext(ctx, "helm repository already stores the chart the upload was rejected for",
				"server", srv.name(), "resource", src.ToIdentity(), "url", uploadURL)
		case known != "" && computed != known:
			if err := srv.discard(ctx, c, computed); err != nil {
				slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
			}
			return nil, fmt.Errorf("digest mismatch: expected %s, got %s", known, computed)
		default:
			slog.InfoContext(ctx, "uploaded helm chart", "server", srv.name(), "resource", src.ToIdentity(), "url", uploadURL)
		}
		digestHex = computed
	}

	name, version, found, err := srv.chart(ctx, c, digestHex)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := srv.discard(ctx, c, digestHex); err != nil {
			slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", uploadURL, "error", err)
		}
		return nil, fmt.Errorf("content of resource %s is not a helm chart: %s recorded no chart name and version for %s", src.ToIdentity(), srv.name(), uploadURL)
	}
	if strings.ContainsAny(name, ":/") || strings.Contains(version, "/") {
		return nil, fmt.Errorf("%s recorded an invalid chart name %q or version %q for %s", srv.name(), name, version, uploadURL)
	}

	srv.afterUpload(ctx, c)

	out := src.DeepCopy()
	out.Access = &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: srv.helmRepository(),
		HelmChart:      name + ":" + version,
	}
	if expected != "" {
		out.Digest = src.Digest.DeepCopy()
	} else {
		out.Digest = &descriptor.Digest{HashAlgorithm: hashAlgorithmSHA256, NormalisationAlgorithm: genericBlobDigestV1, Value: digestHex}
	}
	if transformation.Output == nil {
		transformation.Output = &HelmRepositoryUploadOutput{}
	}
	if transformation.Output.Resource, err = descriptor.ConvertToV2Resource(t.Scheme, out); err != nil {
		return nil, fmt.Errorf("failed converting uploaded resource to v2 format: %w", err)
	}
	return &transformation, nil
}

// chartFile returns the path-escaped file name of the chart:
// <resource name>-<resource version>.tgz, with a hash of the extra identity appended when the
// resource has one, so every resource of a component version has its own file.
func chartFile(res *descriptor.Resource) (string, error) {
	if strings.ContainsAny(res.Name+res.Version, "/\\") {
		return "", fmt.Errorf("resource name %q and version %q must not contain path separators", res.Name, res.Version)
	}
	file := res.Name + "-" + res.Version
	if len(res.ExtraIdentity) > 0 {
		file += fmt.Sprintf("-%016x", res.ExtraIdentity.CanonicalHashV1())
	}
	return url.PathEscape(file + ".tgz"), nil
}

// chartPath returns the path-escaped location of the chart in the repository:
// <component>/<component version>/<chart file>.
func chartPath(component, version string, res *descriptor.Resource) (string, error) {
	file, err := chartFile(res)
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
func (t *HelmRepositoryUpload) localSource(ctx context.Context, cv *HelmRepositoryUploadComponentVersion) (*chartarchive.Local, error) {
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
func (t *HelmRepositoryUpload) resolveSourceCredentials(ctx context.Context, resource *descriptor.Resource) (runtime.Typed, error) {
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
// identity of the published Helm repository, falling back to the Wget identity of the upload
// URL. Without either, the upload is anonymous. HelmHTTPCredentials are mapped to their
// username and password, which is all an HTTP upload uses.
func (t *HelmRepositoryUpload) resolveTargetCredentials(ctx context.Context, helmRepo, putURL string) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	helmID, err := runtime.ParseURLToIdentity(helmRepo)
	if err != nil {
		return nil, fmt.Errorf("failed deriving target consumer identity: %w", err)
	}
	helmID.SetType(helmidentityv1.Type)
	wgetID, err := wgetidentityv1.IdentityFromURL(putURL)
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
		return nil, fmt.Errorf("HelmHTTPCredentials certFile/keyFile are not supported for helm repository uploads; use WgetCredentials/v1 certificate and privateKey")
	}
	return &wgetcredsv1.WgetCredentials{
		Type:     wgetcredsv1.WgetCredentialsVersionedType,
		Username: helmCreds.Username,
		Password: helmCreds.Password,
	}, nil
}

// uploadChart streams the chart archive to putURL and returns the hex SHA-256 of the bytes read
// and whether the archive was read to its end.
func uploadChart(ctx context.Context, c *helmClient, chart *chartarchive.Chart, putURL string, header http.Header) (sha256Hex string, complete bool, err error) {
	rc, err := chart.Archive.ReadCloser()
	if err != nil {
		return "", false, fmt.Errorf("failed opening chart archive: %w", err)
	}
	defer func() { _ = rc.Close() }()
	size := blob.SizeUnknown
	if sized, ok := chart.Archive.(blob.SizeAware); ok {
		size = sized.Size()
	}
	hasher := sha256.New()
	body := &eofReader{r: rc}
	err = c.send(ctx, http.MethodPut, putURL, io.TeeReader(body, hasher), size, header)
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

// helmClient sends authenticated requests to the Helm repository server.
type helmClient struct {
	httpConfig *httpv1alpha1.Config
	creds      runtime.Typed
}

// send issues a single request and fails on a non-2xx response. Errors never carry userinfo,
// query or fragment of target.
func (c *helmClient) send(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header) error {
	resp, err := c.do(ctx, method, target, body, size, header)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		safe := redactURL(target)
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if msg := strings.TrimSpace(string(excerpt)); msg != "" {
			return fmt.Errorf("%s %s returned status %d: %s", method, safe, resp.StatusCode, msg)
		}
		return fmt.Errorf("%s %s returned status %d", method, safe, resp.StatusCode)
	}
	return nil
}

// do sends a single authenticated request. The caller closes the response body.
func (c *helmClient) do(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header) (*http.Response, error) {
	safe := redactURL(target)
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

// redactURL strips userinfo, query and fragment so credentials or presigned parameters never
// reach logs or errors.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
