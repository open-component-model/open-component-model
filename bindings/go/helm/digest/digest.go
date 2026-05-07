package digest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	godigest "github.com/opencontainers/go-digest"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/repo/v1"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helminternal "ocm.software/open-component-model/bindings/go/helm/internal"
	"ocm.software/open-component-model/bindings/go/helm/internal/download"
	"ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const hashAlgorithmSHA256 = "SHA-256"

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for Helm chart access types.
// For HTTP/S repositories it fetches the repository index.yaml and extracts the chart digest.
// For OCI repositories it resolves the OCI manifest digest.
type DigestProcessor struct {
	tempFolder string
}

// NewDigestProcessor creates a new Helm digest processor.
// tempFolder specifies a base directory for temporary files; if empty, os.TempDir() is used.
func NewDigestProcessor(tempFolder string) *DigestProcessor {
	return &DigestProcessor{tempFolder: tempFolder}
}

func (p *DigestProcessor) GetResourceRepositoryScheme() *ocmruntime.Scheme {
	return access.Scheme
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer identity for digest processing.
//
// TODO(matthiasbruns): migrate return type to runtime.Typed once the DigestProcessor interface is updated.
// https://github.com/open-component-model/ocm-project/issues/988
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
//
// TODO(matthiasbruns): migrate credentials parameter to runtime.Typed once the DigestProcessor interface is updated.
// https://github.com/open-component-model/ocm-project/issues/988
func (p *DigestProcessor) ProcessResourceDigest(
	ctx context.Context, resource *runtime.Resource, credentials map[string]string,
) (*runtime.Resource, error) {
	helm := helmv1.Helm{}
	if err := access.Scheme.Convert(resource.Access, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if helm.HelmRepository == "" {
		return nil, fmt.Errorf("helm repository URL is required for digest processing")
	}

	var (
		resolvedDigest godigest.Digest
		err            error
	)

	if strings.HasPrefix(helm.HelmRepository, "oci://") {
		resolvedDigest, err = p.resolveOCIDigest(ctx, helm, ocicredsv1.FromDirectCredentials(credentials))
	} else {
		resolvedDigest, err = p.resolveHTTPDigest(ctx, helm, helmcredsv1.FromDirectCredentials(credentials))
	}
	if err != nil {
		return nil, err
	}

	resource = resource.DeepCopy()

	if resource.Digest == nil {
		resource.Digest = &runtime.Digest{}
		if err := applyDigest(resource.Digest, resolvedDigest); err != nil {
			return nil, fmt.Errorf("failed to apply digest to resource: %w", err)
		}
	} else if err := verifyDigest(resource.Digest, resolvedDigest); err != nil {
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

	chartRepo, err := repo.NewChartRepository(entry, download.GetterProviders())
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
		return "", fmt.Errorf("chart %q version %q in repository index has no digest", chartName, version)
	}

	d, err := parseDigest(cv.Digest)
	if err != nil {
		return "", fmt.Errorf("error parsing chart digest %q: %w", cv.Digest, err)
	}

	return d, nil
}

func (p *DigestProcessor) resolveOCIDigest(ctx context.Context, helm helmv1.Helm, credentials *ocicredsv1.OCICredentials) (godigest.Digest, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context cancelled before resolving OCI digest: %w", err)
	}

	ref, err := helm.ChartReference()
	if err != nil {
		return "", fmt.Errorf("error constructing chart reference: %w", err)
	}

	// Strip the oci:// prefix for the registry client
	ref = strings.TrimPrefix(ref, "oci://")

	var username, password string
	if credentials != nil {
		username = credentials.Username
		password = credentials.Password
		if password == "" {
			if token := credentials.AccessToken; token != "" {
				password = token
			}
		}
	}
	var regClientOpts []registry.ClientOption
	if username != "" && password != "" {
		regClientOpts = append(regClientOpts, registry.ClientOptBasicAuth(username, password))
	}
	regClient, err := registry.NewClient(regClientOpts...)
	if err != nil {
		return "", fmt.Errorf("error creating registry client: %w", err)
	}

	// Helm's registry Resolve does not accept a context, so we run it in a
	// goroutine and select on the context to allow cancellation.
	type resolveResult struct {
		digest godigest.Digest
		err    error
	}
	ch := make(chan resolveResult, 1)
	go func() {
		desc, resolveErr := regClient.Resolve(ref)
		if resolveErr != nil {
			ch <- resolveResult{err: resolveErr}
			return
		}
		ch <- resolveResult{digest: desc.Digest}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("context cancelled while resolving OCI chart reference %q: %w", ref, ctx.Err())
	case res := <-ch:
		if res.err != nil {
			return "", fmt.Errorf("error resolving OCI chart reference %q: %w", ref, res.err)
		}
		return res.digest, nil
	}
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

func applyDigest(target *runtime.Digest, d godigest.Digest) error {
	algo := algorithmName(d.Algorithm())
	if algo == "" {
		return fmt.Errorf("unknown digest algorithm: %s", d.Algorithm())
	}
	target.HashAlgorithm = algo
	target.NormalisationAlgorithm = "genericBlobDigest/v1"
	target.Value = d.Encoded()
	return nil
}

func verifyDigest(target *runtime.Digest, d godigest.Digest) error {
	if target == nil {
		return fmt.Errorf("target digest is nil")
	}
	if target.Value != d.Encoded() {
		return fmt.Errorf("digest value mismatch: expected %s, got %s", target.Value, d.Encoded())
	}
	algo := algorithmName(d.Algorithm())
	if algo == "" {
		return fmt.Errorf("unknown digest algorithm: %s", d.Algorithm())
	}
	if target.HashAlgorithm != algo {
		return fmt.Errorf("hash algorithm mismatch: expected %s, got %s", target.HashAlgorithm, algo)
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
