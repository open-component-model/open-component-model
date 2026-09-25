package internal

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	"ocm.software/open-component-model/bindings/go/blob/compression"
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
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

const (
	JFrogHelmUploadType    = "JFrogHelmUpload"
	jfrogHelmUploadVersion = "v1alpha1"

	// hashAlgorithmSHA256 is the hash algorithm recorded for uploaded chart digests.
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain streamed blob.
	genericBlobDigestV1 = "genericBlobDigest/v1"
	// maxErrorBodyBytes bounds how much of a non-2xx response body is read into an error.
	maxErrorBodyBytes = 4 << 10
	// chartPropertiesAttempts bounds how often the chart metadata Artifactory records on
	// deployment is polled before the content is considered not to be a helm chart.
	chartPropertiesAttempts = 6
	// defaultChartPropertiesInterval is the wait between two chart metadata polls.
	defaultChartPropertiesInterval = 500 * time.Millisecond
)

// JFrogHelmUploadVersionedType is the versioned type identifier for JFrogHelmUpload transformations.
var JFrogHelmUploadVersionedType = runtime.NewVersionedType(JFrogHelmUploadType, jfrogHelmUploadVersion)

// JFrogHelmUploadSpec is the input specification for a JFrogHelmUpload transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type JFrogHelmUploadSpec struct {
	// Resource is the source resource holding the helm chart.
	Resource *descriptorv2.Resource `json:"resource"`
	// ComponentVersion is the component version holding the resource. It determines where the
	// chart is stored in the repository and, for local blob resources, where it is read from.
	ComponentVersion *JFrogHelmUploadComponentVersion `json:"componentVersion"`
	// URL is the Artifactory base URL.
	URL string `json:"url"`
	// Repository is the Artifactory Helm repository key.
	Repository string `json:"repository"`
	// Reindex requests an index recalculation for the uploaded chart. A failure is only logged,
	// because Artifactory indexes deployed charts on its own.
	Reindex bool `json:"reindex,omitempty"`
}

// JFrogHelmUploadComponentVersion identifies the component version holding the resource.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type JFrogHelmUploadComponentVersion struct {
	// Repository is the specification of the repository holding the component version. It is
	// set for local blob resources only, which are read from it.
	Repository *runtime.Raw `json:"repository,omitempty"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
}

// JFrogHelmUploadOutput is the output of a JFrogHelmUpload transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type JFrogHelmUploadOutput struct {
	// Resource is the uploaded resource with its Helm/v1 access on the Artifactory Helm repository.
	Resource *descriptorv2.Resource `json:"resource"`
}

// JFrogHelmUploadTransformation deploys the packaged Helm chart of a resource to a JFrog
// Artifactory Helm repository and publishes the resource with a Helm/v1 access on it.
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type JFrogHelmUploadTransformation struct {
	// +ocm:jsonschema-gen:enum=JFrogHelmUpload/v1alpha1
	Type   runtime.Type           `json:"type"`
	ID     string                 `json:"id"`
	Spec   *JFrogHelmUploadSpec   `json:"spec"`
	Output *JFrogHelmUploadOutput `json:"output,omitempty"`
}

// JFrogHelmUpload deploys the packaged chart of a resource to a JFrog Artifactory Helm
// repository and outputs the resource with a Helm/v1 access on
// <url>/artifactory/api/helm/<repository>. The chart is streamed to
// <url>/artifactory/<repository>/<component>/<component version>/<resource>-<resource version>.tgz
// and never buffered on disk. The chart is not parsed: its name and version are the chart
// metadata Artifactory records when it indexes the deployed chart. Content Artifactory does not
// recognize as a chart is deleted again and fails the transformation.
type JFrogHelmUpload struct {
	Scheme *runtime.Scheme
	Charts *chartarchive.Source
	// ResourceRepository derives the source credential identities of remote resources.
	ResourceRepository repository.ResourceRepository
	// RepoProvider resolves the source repositories of local blob resources.
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
	HTTPConfig         *httpv1alpha1.Config

	// chartPropertiesInterval overrides defaultChartPropertiesInterval (tests).
	chartPropertiesInterval time.Duration
}

func (t *JFrogHelmUpload) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation JFrogHelmUploadTransformation
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to JFrogHelmUpload transformation: %w", err)
	}
	spec := transformation.Spec
	switch {
	case spec == nil:
		return nil, fmt.Errorf("spec is required for JFrogHelmUpload transformation")
	case spec.Resource == nil:
		return nil, fmt.Errorf("source resource is required")
	case spec.ComponentVersion == nil || spec.ComponentVersion.Component == "" || spec.ComponentVersion.Version == "":
		return nil, fmt.Errorf("component and version are required")
	case spec.URL == "":
		return nil, fmt.Errorf("url is required")
	case spec.Repository == "":
		return nil, fmt.Errorf("repository is required")
	}
	src := descriptor.ConvertFromV2Resource(spec.Resource)
	cv := spec.ComponentVersion
	path, err := chartPath(cv.Component, cv.Version, src)
	if err != nil {
		return nil, err
	}
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
	putURL := uploadBase + "/" + path

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

	creds, err := t.resolveTargetCredentials(ctx, helmRepo, putURL)
	if err != nil {
		return nil, err
	}

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

	var digest *descriptor.Digest
	deployed := false
	if known != "" {
		if deployed, err = t.deployByChecksum(ctx, putURL, known, creds); err != nil {
			return nil, err
		}
	}
	if deployed {
		digest = &descriptor.Digest{HashAlgorithm: hashAlgorithmSHA256, NormalisationAlgorithm: genericBlobDigestV1, Value: known}
		slog.InfoContext(ctx, "deployed helm chart to artifactory by checksum without uploading it",
			"resource", src.ToIdentity(), "url", redactURL(putURL))
	} else {
		header := http.Header{"Content-Type": {compression.MediaTypeGzip}}
		if known != "" {
			// Artifactory verifies the uploaded bytes against this checksum and rejects the
			// upload on mismatch, so a corrupted stream is never stored.
			header.Set("X-Checksum-Sha256", known)
		}
		if digest, err = t.upload(ctx, chart, putURL, header, known, creds); err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "uploaded helm chart to artifactory", "resource", src.ToIdentity(), "url", redactURL(putURL))
	}
	if expected != "" {
		digest = src.Digest.DeepCopy()
	}

	name, version, err := t.chartMetadata(ctx, storageBase+"/"+path, creds)
	if err != nil {
		return nil, err
	}
	if name == "" || version == "" {
		if err := t.send(ctx, http.MethodDelete, putURL, nil, -1, nil, creds); err != nil {
			slog.WarnContext(ctx, "failed deleting content that is not a helm chart from artifactory", "url", redactURL(putURL), "error", err)
		}
		return nil, fmt.Errorf("content of resource %s is not a helm chart: artifactory recorded no chart name and version for %s", src.ToIdentity(), redactURL(putURL))
	}
	if strings.ContainsAny(name, ":/") || strings.Contains(version, "/") {
		return nil, fmt.Errorf("artifactory recorded an invalid chart name %q or version %q for %s", name, version, redactURL(putURL))
	}

	if spec.Reindex {
		if err := t.send(ctx, http.MethodPost, helmRepo+"/"+path+"/reindex", nil, -1, nil, creds); err != nil {
			slog.WarnContext(ctx, "failed requesting a helm index recalculation for the uploaded chart; artifactory also indexes deployed charts on its own",
				"url", redactURL(putURL), "error", err)
		}
	}

	out := src.DeepCopy()
	out.Access = &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: helmRepo,
		HelmChart:      name + ":" + version,
	}
	out.Digest = digest
	if transformation.Output == nil {
		transformation.Output = &JFrogHelmUploadOutput{}
	}
	if transformation.Output.Resource, err = descriptor.ConvertToV2Resource(t.Scheme, out); err != nil {
		return nil, fmt.Errorf("failed converting uploaded resource to v2 format: %w", err)
	}
	return &transformation, nil
}

// chartPath returns the path-escaped location of the chart in the repository:
// <component>/<component version>/<resource name>-<resource version>.tgz, with a hash of the
// extra identity appended to the file name when the resource has one, so every resource of a
// component version has its own path.
func chartPath(component, version string, res *descriptor.Resource) (string, error) {
	if strings.ContainsAny(res.Name+res.Version, "/\\") {
		return "", fmt.Errorf("resource name %q and version %q must not contain path separators", res.Name, res.Version)
	}
	file := res.Name + "-" + res.Version
	if len(res.ExtraIdentity) > 0 {
		file += fmt.Sprintf("-%016x", res.ExtraIdentity.CanonicalHashV1())
	}
	segments := append(strings.Split(component, "/"), version, file+".tgz")
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "\\") {
			return "", fmt.Errorf("component %q version %q cannot be used as a repository path", component, version)
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

// chartMetadata reads the chart name and version Artifactory records as properties of a
// deployed chart. It polls briefly in case the metadata is calculated asynchronously and
// returns empty strings when Artifactory recorded none, i.e. the content is not a helm chart.
func (t *JFrogHelmUpload) chartMetadata(ctx context.Context, storageURL string, creds runtime.Typed) (name, version string, err error) {
	interval := t.chartPropertiesInterval
	if interval == 0 {
		interval = defaultChartPropertiesInterval
	}
	target := storageURL + "?properties=chart.name,chart.version"
	for attempt := 1; ; attempt++ {
		resp, err := t.do(ctx, http.MethodGet, target, nil, -1, nil, creds)
		if err != nil {
			return "", "", err
		}
		var props struct {
			Properties map[string][]string `json:"properties"`
		}
		switch resp.StatusCode {
		case http.StatusOK:
			err = json.NewDecoder(io.LimitReader(resp.Body, maxErrorBodyBytes)).Decode(&props)
			_ = resp.Body.Close()
			if err != nil {
				return "", "", fmt.Errorf("failed decoding chart properties of %s: %w", redactURL(storageURL), err)
			}
			if names, versions := props.Properties["chart.name"], props.Properties["chart.version"]; len(names) == 1 && len(versions) == 1 {
				return names[0], versions[0], nil
			}
		case http.StatusNotFound:
			_ = resp.Body.Close()
		default:
			_ = resp.Body.Close()
			return "", "", fmt.Errorf("GET %s returned status %d", redactURL(target), resp.StatusCode)
		}
		if attempt == chartPropertiesAttempts {
			return "", "", nil
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(interval):
		}
	}
}

// localSource resolves the source component version repository of a local blob resource.
func (t *JFrogHelmUpload) localSource(ctx context.Context, cv *JFrogHelmUploadComponentVersion) (*chartarchive.Local, error) {
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
func (t *JFrogHelmUpload) resolveSourceCredentials(ctx context.Context, resource *descriptor.Resource) (runtime.Typed, error) {
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
// identity of the Artifactory Helm repository, falling back to the Wget identity of the upload
// URL. Without either, the upload is anonymous. HelmHTTPCredentials are mapped to their
// username and password, which is all an HTTP upload uses.
func (t *JFrogHelmUpload) resolveTargetCredentials(ctx context.Context, helmRepo, putURL string) (runtime.Typed, error) {
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
		return nil, fmt.Errorf("HelmHTTPCredentials certFile/keyFile are not supported for JFrog uploads; use WgetCredentials/v1 certificate and privateKey")
	}
	return &wgetcredsv1.WgetCredentials{
		Type:     wgetcredsv1.WgetCredentialsVersionedType,
		Username: helmCreds.Username,
		Password: helmCreds.Password,
	}, nil
}

// upload streams the chart archive into putURL and returns the digest of the uploaded bytes,
// which must equal expected when set.
func (t *JFrogHelmUpload) upload(ctx context.Context, chart *chartarchive.Chart, putURL string, header http.Header, expected string, creds runtime.Typed) (*descriptor.Digest, error) {
	rc, err := chart.Archive.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening chart archive: %w", err)
	}
	defer func() { _ = rc.Close() }()
	size := blob.SizeUnknown
	if sized, ok := chart.Archive.(blob.SizeAware); ok {
		size = sized.Size()
	}
	hasher := sha256.New()
	if err := t.send(ctx, http.MethodPut, putURL, io.TeeReader(rc, hasher), size, header, creds); err != nil {
		return nil, err
	}
	computed := godigest.NewDigestFromBytes(godigest.SHA256, hasher.Sum(nil)).Encoded()
	if expected != "" && computed != expected {
		return nil, fmt.Errorf("digest mismatch: expected %s, got %s", expected, computed)
	}
	return &descriptor.Digest{HashAlgorithm: hashAlgorithmSHA256, NormalisationAlgorithm: genericBlobDigestV1, Value: computed}, nil
}

// deployByChecksum asks Artifactory to deploy putURL from content it already stores under the
// checksum ("Deploy Artifact by Checksum"), so the chart is not uploaded again. It reports
// false when Artifactory does not have the content (404) or declines the request otherwise;
// the caller then uploads the chart, which surfaces real errors such as missing permissions.
func (t *JFrogHelmUpload) deployByChecksum(ctx context.Context, putURL, checksum string, creds runtime.Typed) (bool, error) {
	resp, err := t.do(ctx, http.MethodPut, putURL, nil, 0, http.Header{
		"X-Checksum-Deploy": {"true"},
		"X-Checksum-Sha256": {checksum},
	}, creds)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// send issues a single request to Artifactory and fails on a non-2xx response. Errors never
// carry userinfo, query or fragment of target.
func (t *JFrogHelmUpload) send(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header, creds runtime.Typed) error {
	resp, err := t.do(ctx, method, target, body, size, header, creds)
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

// do sends a single authenticated request to Artifactory. The caller closes the response body.
func (t *JFrogHelmUpload) do(ctx context.Context, method, target string, body io.Reader, size int64, header http.Header, creds runtime.Typed) (*http.Response, error) {
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
	client := ocmhttp.New(ocmhttp.WithConfig(t.HTTPConfig))
	if err := httpauth.Apply(ctx, req, &client, creds); err != nil {
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
