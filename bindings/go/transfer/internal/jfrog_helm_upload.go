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
)

// JFrogHelmUploadVersionedType is the versioned type identifier for JFrogHelmUpload transformations.
var JFrogHelmUploadVersionedType = runtime.NewVersionedType(JFrogHelmUploadType, jfrogHelmUploadVersion)

// JFrogHelmUploadSpec is the input specification for a JFrogHelmUpload transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type JFrogHelmUploadSpec struct {
	// Resource is the source resource holding the helm chart.
	Resource *descriptorv2.Resource `json:"resource"`
	// ComponentVersion locates the source component version of a local blob resource.
	ComponentVersion *JFrogHelmUploadComponentVersion `json:"componentVersion,omitempty"`
	// URL is the Artifactory base URL.
	URL string `json:"url"`
	// Repository is the Artifactory Helm repository key.
	Repository string `json:"repository"`
	// Reindex requests a reindex of the Helm repository after the upload.
	Reindex bool `json:"reindex,omitempty"`
}

// JFrogHelmUploadComponentVersion identifies the component version holding a local resource.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type JFrogHelmUploadComponentVersion struct {
	// Repository is the specification of the repository holding the component version.
	Repository *runtime.Raw `json:"repository"`
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

// JFrogHelmUpload streams the packaged chart of a resource into
// PUT <url>/artifactory/<repository>/<name>-<version>.tgz, with name and version taken from the
// chart's own metadata, optionally reindexes the repository and outputs the resource with a
// Helm/v1 access on <url>/artifactory/api/helm/<repository>. The chart is never buffered on disk.
type JFrogHelmUpload struct {
	Scheme *runtime.Scheme
	Charts *chartarchive.Source
	// ResourceRepository derives the source credential identities of remote resources.
	ResourceRepository repository.ResourceRepository
	// RepoProvider resolves the source repositories of local blob resources.
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
	HTTPConfig         *httpv1alpha1.Config
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
	case spec.URL == "":
		return nil, fmt.Errorf("url is required")
	case spec.Repository == "":
		return nil, fmt.Errorf("repository is required")
	}
	uploadBase, err := url.JoinPath(spec.URL, "artifactory", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}
	helmRepo, err := url.JoinPath(spec.URL, "artifactory", "api", "helm", spec.Repository)
	if err != nil {
		return nil, fmt.Errorf("invalid artifactory url: %w", err)
	}

	src := descriptor.ConvertFromV2Resource(spec.Resource)
	req := chartarchive.Request{Resource: src}
	if cv := spec.ComponentVersion; cv != nil {
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
	putURL := uploadBase + "/" + chart.Name + "-" + chart.Version + ".tgz"

	creds, err := t.resolveTargetCredentials(ctx, helmRepo, putURL)
	if err != nil {
		return nil, err
	}

	rc, err := chart.Archive.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening chart archive of resource %s: %w", src.ToIdentity(), err)
	}
	defer func() { _ = rc.Close() }()
	size := blob.SizeUnknown
	if sized, ok := chart.Archive.(blob.SizeAware); ok {
		size = sized.Size()
	}
	slog.InfoContext(ctx, "uploading helm chart to artifactory",
		"resource", src.ToIdentity(), "chart", chart.Name+":"+chart.Version, "url", redactURL(putURL))
	hasher := sha256.New()
	if err := t.send(ctx, http.MethodPut, putURL, io.TeeReader(rc, hasher), size, compression.MediaTypeGzip, creds); err != nil {
		return nil, err
	}

	computed := godigest.NewDigestFromBytes(godigest.SHA256, hasher.Sum(nil)).Encoded()
	digest, err := uploadedDigest(src.Digest, chart.FromOCI, computed)
	if err != nil {
		return nil, err
	}

	if spec.Reindex {
		if err := t.send(ctx, http.MethodPost, helmRepo+"/reindex", nil, -1, "", creds); err != nil {
			return nil, err
		}
	}

	out := src.DeepCopy()
	out.Access = &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: helmRepo,
		HelmChart:      chart.Name + ":" + chart.Version,
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

// uploadedDigest records the digest of the uploaded chart. A source digest that describes the
// uploaded bytes is verified against them; a chart extracted from an OCI artifact has a
// different byte representation, so its computed digest is recorded instead.
func uploadedDigest(src *descriptor.Digest, fromOCI bool, computed string) (*descriptor.Digest, error) {
	if src == nil || fromOCI {
		return &descriptor.Digest{HashAlgorithm: hashAlgorithmSHA256, NormalisationAlgorithm: genericBlobDigestV1, Value: computed}, nil
	}
	if src.HashAlgorithm != hashAlgorithmSHA256 {
		return nil, fmt.Errorf("unsupported hash algorithm: expected %s, got %s", hashAlgorithmSHA256, src.HashAlgorithm)
	}
	if src.NormalisationAlgorithm != genericBlobDigestV1 {
		return nil, fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, src.NormalisationAlgorithm)
	}
	if src.Value != computed {
		return nil, fmt.Errorf("digest mismatch: expected %s, got %s", src.Value, computed)
	}
	return src.DeepCopy(), nil
}

// localSource resolves the source component version repository of a local blob resource.
func (t *JFrogHelmUpload) localSource(ctx context.Context, cv *JFrogHelmUploadComponentVersion) (*chartarchive.Local, error) {
	if cv.Repository == nil || cv.Component == "" || cv.Version == "" {
		return nil, fmt.Errorf("component version repository, component and version are required for local resources")
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

// send issues a single request to Artifactory. Errors never carry userinfo, query or fragment
// of target.
func (t *JFrogHelmUpload) send(ctx context.Context, method, target string, body io.Reader, size int64, contentType string, creds runtime.Typed) error {
	safe := redactURL(target)
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("failed creating %s request for %s: %w", method, safe, err)
	}
	if size >= 0 {
		req.ContentLength = size
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := ocmhttp.New(ocmhttp.WithConfig(t.HTTPConfig))
	if err := httpauth.Apply(ctx, req, &client, creds); err != nil {
		return fmt.Errorf("failed applying target credentials: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		// client.Do wraps errors in a *url.Error carrying the full request URL.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = safe
		}
		return fmt.Errorf("%s %s failed: %w", method, safe, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if msg := strings.TrimSpace(string(excerpt)); msg != "" {
			return fmt.Errorf("%s %s returned status %d: %s", method, safe, resp.StatusCode, msg)
		}
		return fmt.Errorf("%s %s returned status %d", method, safe, resp.StatusCode)
	}
	return nil
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
