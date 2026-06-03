package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/downloader"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/registry"
	helmrepo "helm.sh/helm/v4/pkg/repo/v1"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm/internal"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// NewReadOnlyChartFromRemote downloads a Helm chart from a remote repository and returns it as [helm.ChartData].
// The helmRepo parameter accepts both OCI references (e.g. "oci://registry.example.com/charts/mychart:1.0.0")
// and HTTP/S URLs (e.g. "https://example.com/charts/mychart-1.0.0.tgz").
// The targetDir parameter specifies the directory where the chart will be downloaded and processed. The caller is responsible for cleaning up this directory after use.
func NewReadOnlyChartFromRemote(ctx context.Context, helmRepo, targetDir string, opts ...Option) (result *internal.ChartData, err error) {
	if helmRepo == "" {
		return nil, errors.New("helm repository must be specified")
	}
	if targetDir == "" {
		return nil, errors.New("target directory must be specified")
	}

	opt := &option{}
	for _, o := range opts {
		o(opt)
	}

	if opt.Credentials == nil {
		opt.Credentials = &helmcredsv1.HelmHTTPCredentials{}
	}
	if opt.OCICredentials == nil {
		opt.OCICredentials = &ocicredsv1.OCICredentials{}
	}

	chartDir, err := os.MkdirTemp(targetDir, "helmRemoteChart*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory: %w", err)
	}

	var getterOpts []getter.Option

	tlsOpts := []tlOptionsFn{
		withCACertFile(opt.CACertFile),
		withCACert(opt.CACert),
		withCredentials(opt.Credentials),
	}
	tlsOption, err := constructTLSOptions(targetDir, tlsOpts...)
	if err != nil {
		return nil, fmt.Errorf("error setting up TLS options: %w", err)
	}
	getterOpts = append(getterOpts, tlsOption)

	var (
		keyring string
		verify  = downloader.VerifyNever
	)

	if opt.AlwaysDownloadProv {
		// At least download the .prov file
		verify = downloader.VerifyLater
	}

	if opt.Credentials.Keyring != "" {
		keyring = opt.Credentials.Keyring
		// We set verifyIfPossible to allow the download to run verify if keyring is defined. Without the keyring
		// verification would not be possible at all.
		// https://github.com/open-component-model/ocm/blob/be847549af3d2947a2c8bc2b38d51a20c2a8a9ba/api/tech/helm/downloader.go#L128
		verify = downloader.VerifyIfPossible
	}

	var plainHTTP bool
	if strings.HasPrefix(helmRepo, "http://") {
		slog.WarnContext(ctx, "using plain HTTP for chart download")
		plainHTTP = true
	}

	getterOpts = append(getterOpts, getter.WithPlainHTTP(plainHTTP))

	regClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("error creating registry client: %w", err)
	}

	cacheDir, err := os.MkdirTemp(targetDir, "helm-cache*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory for helm operations: %w", err)
	}
	defer func(path string) {
		_ = os.RemoveAll(path)
	}(cacheDir)

	dl := &downloader.ChartDownloader{
		Out:     os.Stderr,
		Verify:  verify,
		Getters: GetterProviders(),
		// set by ocm v1 originally.
		RepositoryCache:  filepath.Join(cacheDir, ".helmcache"),
		RepositoryConfig: filepath.Join(cacheDir, ".helmrepo"),
		ContentCache:     filepath.Join(cacheDir, ".helmcontent"),
		RegistryClient:   regClient,
		Options:          getterOpts,
		Keyring:          keyring,
	}

	// Do not break legacy behaviour, but also support pure OCI based credentials
	username := opt.Credentials.Username
	if username == "" {
		username = opt.OCICredentials.Username
	}
	password := opt.Credentials.Password
	if password == "" {
		password = opt.OCICredentials.Password
	}
	if password == "" {
		password = opt.OCICredentials.AccessToken
	}

	if username != "" && password != "" {
		dl.Options = append(dl.Options, getter.WithBasicAuth(username, password))
	}

	resolvedRepo, err := resolveHTTPChartURL(ctx, helmRepo, opt.Version, targetDir, GetterProviders(), getterOpts)
	if err != nil {
		return nil, fmt.Errorf("error resolving chart URL %q via index.yaml: %w", helmRepo, err)
	}

	version, err := getVersion(opt.Version, resolvedRepo)
	if err != nil {
		return nil, fmt.Errorf("error determining chart version: %w", err)
	}

	savedPath, _, err := dl.DownloadTo(resolvedRepo, version, chartDir)
	if err != nil {
		return nil, fmt.Errorf("error downloading chart %q version %q: %w", helmRepo, version, err)
	}

	chart, err := loader.Load(savedPath)
	if err != nil {
		return nil, fmt.Errorf("error loading downloaded chart from %q: %w", savedPath, err)
	}

	result = &internal.ChartData{
		Name:     chart.Name(),
		Version:  chart.Metadata.Version,
		ChartDir: chartDir,
	}

	if result.ChartBlob, err = filesystem.GetBlobFromOSPath(savedPath); err != nil {
		return nil, fmt.Errorf("error creating blob from downloaded chart %q: %w", savedPath, err)
	}
	provPath := savedPath + ".prov"
	if _, err := os.Stat(provPath); err == nil {
		if result.ProvBlob, err = filesystem.GetBlobFromOSPath(provPath); err != nil {
			return nil, fmt.Errorf("error creating blob from provenance file %q: %w", provPath, err)
		}
	}

	return result, nil
}

// GetterProviders returns the available getter providers.
// This replaces the need for cli.New() and avoids the explosion of the dependency tree.
func GetterProviders() getter.Providers {
	return getter.Providers{
		{
			Schemes: []string{"http", "https"},
			New: func(options ...getter.Option) (getter.Getter, error) {
				options = append(options, defaultOptions...)
				return getter.NewHTTPGetter(options...)
			},
		},
		{
			Schemes: []string{registry.OCIScheme},
			New:     getter.NewOCIGetter,
		},
	}
}

// getVersion determines the version of the chart to download based on the provided version override and the helm repository URL.
// We don't let helm download decide on the version of the chart. Version, either through ref or through
// the spec.Version field always MUST be defined. This is only true for OCI repositories.
// In the case of HTTP/S repositories, the version is taken from the URL.
func getVersion(versionOverride, helmRepo string) (string, error) {
	if versionOverride == "" && strings.HasPrefix(helmRepo, "oci://") {
		stripped := strings.TrimPrefix(helmRepo, "oci://")
		ref, err := looseref.ParseReference(stripped)
		if err != nil {
			return "", fmt.Errorf("error parsing helm repository reference %q: %w", helmRepo, err)
		}

		if ref.Tag == "" {
			return "", errors.New("either helm repository tag or spec.Version has to be set")
		}

		return ref.Tag, nil
	}

	return versionOverride, nil
}

// resolveHTTPChartURL resolves the real download URL for an HTTP/S Helm repo reference
// of the form <scheme>://<host>/<repoPath>/<chartName>:<version> — the output of
// (*v1.Helm).ChartReference(). It fetches index.yaml, looks up the chart entry, and
// returns the absolute URL from urls[0].
//
// Returns helmRepo unchanged when no resolution was possible.
func resolveHTTPChartURL(ctx context.Context, helmRepo, requestedVersion, tmpDir string, providers getter.Providers, getterOpts []getter.Option) (string, error) {
	// resolveHTTPChartURL is called speculatively: helmRepo may be a direct .tgz URL,
	// an OCI reference, or any form not produced by ChartReference(). All of those are
	// passed through unchanged so that helm's DownloadTo can handle them directly.
	if !strings.HasPrefix(helmRepo, "http://") && !strings.HasPrefix(helmRepo, "https://") {
		return helmRepo, nil
	}

	ref, err := looseref.ParseReference(helmRepo)
	if err != nil {
		return helmRepo, nil
	}

	// Tag holds the version; Repository holds "<host>/<repoPath>/<chartName>".
	// If either is absent this isn't a ChartReference()-style URL.
	if ref.Tag == "" || ref.Repository == "" {
		return helmRepo, nil
	}

	// chartName is the last path segment of the repository, repoPath is everything before it.
	chartName := path.Base(ref.Repository)
	repoPath := path.Dir(ref.Repository)
	base := &url.URL{
		Scheme: ref.Scheme,
		Host:   ref.Registry,
	}
	if repoPath != "." {
		base.Path = "/" + repoPath
	}
	repoBase := base.String()
	chartVersion := ref.Tag
	if requestedVersion != "" {
		chartVersion = requestedVersion
	}

	indexURL, err := helmrepo.ResolveReferenceURL(repoBase, "index.yaml")
	if err != nil {
		return "", fmt.Errorf("error constructing index.yaml URL for %q: %w", repoBase, err)
	}

	scheme := ref.Scheme
	g, err := providers.ByScheme(scheme)
	if err != nil {
		return "", fmt.Errorf("no getter for scheme %q: %w", scheme, err)
	}

	slog.DebugContext(ctx, "fetching Helm repository index", "url", indexURL)

	buf, err := g.Get(indexURL, getterOpts...)
	if err != nil {
		return "", fmt.Errorf("error fetching index.yaml from %q: %w", indexURL, err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "helm-index-*.yaml")
	if err != nil {
		return "", fmt.Errorf("error creating temp file for index.yaml: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err = io.Copy(tmpFile, buf); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("error writing index.yaml to temp file: %w", err)
	}
	if err = tmpFile.Close(); err != nil {
		return "", fmt.Errorf("error closing index.yaml temp file: %w", err)
	}

	index, err := helmrepo.LoadIndexFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("error parsing index.yaml from %q: %w", repoBase, err)
	}

	cv, err := index.Get(chartName, chartVersion)
	if err != nil {
		return "", fmt.Errorf("chart %q version %q not found in index at %q: %w", chartName, chartVersion, repoBase, err)
	}

	if len(cv.URLs) == 0 {
		return "", fmt.Errorf("chart %q version %q has no download URLs in index at %q", chartName, chartVersion, repoBase)
	}

	absURL, err := helmrepo.ResolveReferenceURL(repoBase, cv.URLs[0])
	if err != nil {
		return "", fmt.Errorf("error resolving chart URL %q against base %q: %w", cv.URLs[0], repoBase, err)
	}

	return absURL, nil
}
