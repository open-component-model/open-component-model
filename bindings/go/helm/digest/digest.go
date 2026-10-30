package digest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	godigest "github.com/opencontainers/go-digest"
	"helm.sh/helm/v4/pkg/repo/v1"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helminternal "ocm.software/open-component-model/bindings/go/helm/internal"
	"ocm.software/open-component-model/bindings/go/helm/internal/download"
	"ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcreds "ocm.software/open-component-model/bindings/go/helm/spec/credentials"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	hashAlgorithmSHA256 = "SHA-256"
	genericBlobDigestV1 = "genericBlobDigest/v1"
	ociArtifactDigestV1 = "ociArtifactDigest/v1"
)

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for Helm chart access types.
// For HTTP/S repositories it fetches the repository index.yaml and extracts the chart digest.
// For OCI repositories it resolves the OCI manifest digest.
type DigestProcessor struct {
	tempFolder string
	httpConfig *httpv1alpha1.Config
}

// Option configures a [DigestProcessor].
type Option func(*DigestProcessor)

// WithHTTPConfig sets the HTTP client configuration for every request the processor
// makes: OCI manifest resolution, repository index and chart downloads.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(p *DigestProcessor) { p.httpConfig = cfg }
}

// NewDigestProcessor creates a new Helm digest processor.
// tempFolder specifies a base directory for temporary files; if empty, os.TempDir() is used.
func NewDigestProcessor(tempFolder string, opts ...Option) *DigestProcessor {
	p := &DigestProcessor{tempFolder: tempFolder}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *DigestProcessor) GetResourceRepositoryScheme() *ocmruntime.Scheme {
	return access.Scheme
}

// GetCredentialTypeScheme returns the credential payload types this digest processor consumes
// (currently HelmHTTPCredentials/v1). oci:// helm repositories are authenticated with
// OCICredentials/v1, which is declared by the OCI binding.
func (p *DigestProcessor) GetCredentialTypeScheme() *ocmruntime.Scheme {
	return helmcreds.Scheme
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer identity for digest processing.
func (p *DigestProcessor) GetResourceDigestProcessorCredentialConsumerIdentity(
	ctx context.Context, resource *runtime.Resource,
) (ocmruntime.Identity, error) {
	helm := helmv1.Helm{}
	if err := access.Scheme.Convert(resource.Access, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if helm.HelmRepository == "" {
		slog.DebugContext(ctx, "local helm inputs do not require credentials")
		return nil, nil
	}

	return helminternal.CredentialConsumerIdentity(helm.HelmRepository)
}

// ProcessResourceDigest resolves the digest of a Helm chart resource.
func (p *DigestProcessor) ProcessResourceDigest(
	ctx context.Context, resource *runtime.Resource, credentials ocmruntime.Typed,
) (*runtime.Resource, error) {
	helm := helmv1.Helm{}
	if err := access.Scheme.Convert(resource.Access, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if helm.HelmRepository == "" {
		return nil, fmt.Errorf("helm repository URL is required for digest processing")
	}

	var resolvedDigest godigest.Digest
	normalisation := genericBlobDigestV1

	var err error
	if strings.HasPrefix(helm.HelmRepository, "oci://") {
		normalisation = ociArtifactDigestV1
		var ociCreds *ocicredsv1.OCICredentials
		if credentials != nil {
			if ociCreds, err = helmcredsv1.ConvertToOCICredentials(credentials); err != nil {
				return nil, fmt.Errorf("error converting credentials: %w", err)
			}
		}
		var ref string
		if ref, err = helm.ChartReference(); err != nil {
			return nil, fmt.Errorf("error constructing chart reference: %w", err)
		}
		resolvedDigest, err = download.ResolveOCIManifestDigest(ctx, ref, ociCreds, p.httpConfig)
	} else {
		var helmCreds *helmcredsv1.HelmHTTPCredentials
		if credentials != nil {
			if helmCreds, err = helmcredsv1.ConvertToHelmHTTPCredentials(credentials); err != nil {
				return nil, fmt.Errorf("error converting credentials: %w", err)
			}
		}
		resolvedDigest, err = p.resolveHTTPDigest(ctx, helm, helmCreds)
	}
	if err != nil {
		return nil, err
	}

	resource = resource.DeepCopy()

	if resource.Digest == nil {
		resource.Digest = &runtime.Digest{}
		if err := applyDigest(resource.Digest, resolvedDigest, normalisation); err != nil {
			return nil, fmt.Errorf("failed to apply digest to resource: %w", err)
		}
	} else if err := verifyDigest(resource.Digest, resolvedDigest, normalisation); err != nil {
		return nil, fmt.Errorf("failed to verify digest of resource: %w", err)
	}

	return resource, nil
}

func (p *DigestProcessor) resolveHTTPDigest(ctx context.Context, helm helmv1.Helm, credentials *helmcredsv1.HelmHTTPCredentials) (godigest.Digest, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context cancelled before resolving HTTP digest: %w", err)
	}

	tempDir := p.tempFolder
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	cacheDir, err := os.MkdirTemp(tempDir, "helm-digest-cache-*")
	if err != nil {
		return "", fmt.Errorf("error creating temporary directory: %w", err)
	}
	defer func(path string) {
		if err := os.RemoveAll(path); err != nil {
			slog.WarnContext(ctx, "failed to remove temporary helm digest cache directory", "path", path, "err", err)
		}
	}(cacheDir)

	entry := &repo.Entry{
		Name: "digest-resolver",
		URL:  helm.HelmRepository,
	}

	if credentials != nil {
		entry.Username = credentials.Username
		entry.Password = credentials.Password
		entry.CertFile = credentials.CertFile
		entry.KeyFile = credentials.KeyFile
	}

	providers, err := download.IndexGetterProviders(helm.HelmRepository, credentials, p.httpConfig)
	if err != nil {
		return "", fmt.Errorf("error creating helm repository client: %w", err)
	}
	chartRepo, err := repo.NewChartRepository(entry, providers)
	if err != nil {
		return "", fmt.Errorf("error creating chart repository: %w", err)
	}
	chartRepo.CachePath = cacheDir

	// Helm's DownloadIndexFile does not accept a context, so we run it in a
	// goroutine and select on the context to allow cancellation.
	type indexResult struct {
		path string
		err  error
	}
	ch := make(chan indexResult, 1)
	go func() {
		idxPath, dlErr := chartRepo.DownloadIndexFile()
		ch <- indexResult{path: idxPath, err: dlErr}
	}()

	var idxPath string
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("context cancelled while downloading repository index: %w", ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return "", fmt.Errorf("error downloading repository index: %w", res.err)
		}
		idxPath = res.path
	}

	indexFile, err := repo.LoadIndexFile(idxPath)
	if err != nil {
		return "", fmt.Errorf("error loading repository index: %w", err)
	}

	chartName := helm.GetChartName()
	version := helm.GetVersion()

	cv, err := indexFile.Get(chartName, version)
	if err != nil {
		return "", fmt.Errorf("chart %q version %q not found in repository index: %w", chartName, version, err)
	}

	if cv.Digest == "" {
		// The index does not vouch for the chart, so it is hashed as it is downloaded.
		slog.InfoContext(ctx, "helm repository index lists no digest for the chart; downloading it to compute one",
			"chart", chartName, "version", version)
		return p.downloadChartDigest(ctx, helm, credentials, cacheDir)
	}

	d, err := parseDigest(cv.Digest)
	if err != nil {
		return "", fmt.Errorf("error parsing chart digest %q: %w", cv.Digest, err)
	}

	return d, nil
}

// downloadChartDigest downloads the chart .tgz into dir and returns its digest.
func (p *DigestProcessor) downloadChartDigest(ctx context.Context, helm helmv1.Helm, credentials *helmcredsv1.HelmHTTPCredentials, dir string) (godigest.Digest, error) {
	ref, err := helm.ChartReference()
	if err != nil {
		return "", fmt.Errorf("error constructing chart reference: %w", err)
	}
	var opts []download.Option
	if credentials != nil {
		opts = append(opts, download.WithCredentials(credentials))
	}
	if p.httpConfig != nil {
		opts = append(opts, download.WithHTTPConfig(p.httpConfig))
	}
	chart, err := download.NewReadOnlyChartFromRemote(ctx, ref, dir, opts...)
	if err != nil {
		return "", fmt.Errorf("error downloading helm chart %q: %w", ref, err)
	}
	rc, err := chart.ChartBlob.ReadCloser()
	if err != nil {
		return "", fmt.Errorf("error reading helm chart %q: %w", ref, err)
	}
	d, err := godigest.SHA256.FromReader(rc)
	if err = errors.Join(err, rc.Close()); err != nil {
		return "", fmt.Errorf("error hashing helm chart %q: %w", ref, err)
	}
	return d, nil
}

// parseDigest parses a digest string that may be either in the standard
// algorithm:hex format (e.g. "sha256:abc123...") or a bare hex string
// (common in Helm repository index files). Bare hex strings are assumed
// to be SHA-256.
func parseDigest(raw string) (godigest.Digest, error) {
	if strings.Contains(raw, ":") {
		return godigest.Parse(raw)
	}
	// Bare hex string — assume SHA-256
	d := godigest.NewDigestFromEncoded(godigest.SHA256, raw)
	return d, d.Validate()
}

func applyDigest(target *runtime.Digest, d godigest.Digest, normalisation string) error {
	algo := algorithmName(d.Algorithm())
	if algo == "" {
		return fmt.Errorf("unknown digest algorithm: %s", d.Algorithm())
	}
	target.HashAlgorithm = algo
	target.NormalisationAlgorithm = normalisation
	target.Value = d.Encoded()
	return nil
}

func verifyDigest(target *runtime.Digest, d godigest.Digest, normalisation string) error {
	if target == nil {
		return fmt.Errorf("target digest is nil")
	}
	complete := target.HashAlgorithm != "" && target.NormalisationAlgorithm != "" && target.Value != ""
	if target.NormalisationAlgorithm != "" && target.NormalisationAlgorithm != normalisation &&
		(normalisation != ociArtifactDigestV1 || target.NormalisationAlgorithm != genericBlobDigestV1) {
		return fmt.Errorf("normalisation algorithm mismatch: expected %s, got %s", normalisation, target.NormalisationAlgorithm)
	}
	if target.Value != "" && target.Value != d.Encoded() {
		return fmt.Errorf("digest value mismatch: expected %s, got %s", target.Value, d.Encoded())
	}
	algo := algorithmName(d.Algorithm())
	if algo == "" {
		return fmt.Errorf("unknown digest algorithm: %s", d.Algorithm())
	}
	if target.HashAlgorithm != "" && target.HashAlgorithm != algo &&
		(complete || !strings.EqualFold(strings.ReplaceAll(target.HashAlgorithm, "-", ""), strings.ReplaceAll(algo, "-", ""))) {
		return fmt.Errorf("hash algorithm mismatch: expected %s, got %s", target.HashAlgorithm, algo)
	}
	if !complete {
		return applyDigest(target, d, normalisation)
	}
	return nil
}

func algorithmName(algo godigest.Algorithm) string {
	switch algo {
	case godigest.SHA256:
		return hashAlgorithmSHA256
	default:
		return ""
	}
}
