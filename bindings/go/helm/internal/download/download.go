package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
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

	var helmGetterOpts []getter.Option

	tlsOpts := []tlOptionsFn{
		withCACertFile(opt.CACertFile),
		withCACert(opt.CACert),
		withCredentials(opt.Credentials),
	}
	tlsOption, caFile, err := constructTLSOptions(targetDir, tlsOpts...)
	if err != nil {
		return nil, fmt.Errorf("error setting up TLS options: %w", err)
	}
	helmGetterOpts = append(helmGetterOpts, tlsOption)

	var (
		keyring string
		verify  = downloader.VerifyNever
	)

	if opt.AlwaysDownloadProv {
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

	helmGetterOpts = append(helmGetterOpts, getter.WithPlainHTTP(plainHTTP))

	// Resolve credentials early so they can be passed to GetterProviders.
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

	var regClientOpts []registry.ClientOption
	var httpClient *http.Client
	if opt.HTTPConfig != nil {
		// Build a single client from the full config (includes per-host routing).
		// It is shared between the OCI registry client and the HTTP/S getter so
		// both paths use the same timeout and per-host override behaviour.
		httpClient = ocmhttp.New(ocmhttp.WithConfig(opt.HTTPConfig))
		regClientOpts = append(regClientOpts, registry.ClientOptHTTPClient(httpClient))
	}
	regClient, err := registry.NewClient(regClientOpts...)
	if err != nil {
		return nil, fmt.Errorf("error creating registry client: %w", err)
	}

	cfgOpts := HTTPConfigGetterOpts{
		username: username,
		password: password,
		baseURL:  helmRepo,
	}

	cacheDir, err := os.MkdirTemp(targetDir, "helm-cache*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory for helm operations: %w", err)
	}
	defer func(path string) {
		_ = os.RemoveAll(path)
	}(cacheDir)

	providers := GetterProviders(httpClient, cfgOpts)

	dl := &downloader.ChartDownloader{
		Out:     os.Stderr,
		Verify:  verify,
		Getters: providers,
		// set by ocm v1 originally.
		RepositoryCache:  filepath.Join(cacheDir, ".helmcache"),
		RepositoryConfig: filepath.Join(cacheDir, ".helmrepo"),
		ContentCache:     filepath.Join(cacheDir, ".helmcontent"),
		RegistryClient:   regClient,
		Options:          helmGetterOpts,
		Keyring:          keyring,
	}

	resolvedRepo, err := resolveHTTPChartURL(ctx, helmRepo, opt.Version, targetDir, GetterProviders(httpClient, cfgOpts), &helmrepo.Entry{
		Name:     "index",
		Username: username,
		Password: password,
		CertFile: opt.Credentials.CertFile,
		KeyFile:  opt.Credentials.KeyFile,
		CAFile:   caFile,
	})
	if err != nil {
		return nil, fmt.Errorf("error resolving chart URL %q via index.yaml: %w", helmRepo, err)
	}

	// Update baseURL to the resolved repo URL for accurate same-host credential scoping,
	// then rebuild providers so httpConfigGetter instances capture the new baseURL.
	cfgOpts.baseURL = resolvedRepo
	if httpClient != nil {
		providers = GetterProviders(httpClient, cfgOpts)
		dl.Getters = providers
	}

	// For the standard getter.HTTPGetter path (no custom client), credentials
	// must be forwarded via dl.Options. The httpConfigGetter path has them
	// baked in via cfgOpts above.
	if httpClient == nil && username != "" && password != "" && sameHost(helmRepo, resolvedRepo) {
		dl.Options = append(dl.Options, getter.WithBasicAuth(username, password))
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
// When client is non-nil, the HTTP/S provider uses a custom getter backed by
// that client so per-host routing and full timeout config apply on the HTTP/S
// path as well as the OCI path. The opts struct carries the credential and
// header values that httpConfigGetter needs, bypassing the opaque
// getter.Option type.
func GetterProviders(client *http.Client, opts HTTPConfigGetterOpts) getter.Providers {
	providers := getter.Providers{
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
	if client != nil {
		providers[0].New = func(_ ...getter.Option) (getter.Getter, error) {
			return NewHTTPConfigGetter(client, opts)
		}
	}
	return providers
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
// (*v1.Helm).ChartReference(). Most of this logic is replicated from `FindChartInRepoURL`.
// Looks up the chart entry and returns the absolute URL from urls[0]. The reason for extracting
// that logic and not using directly, is so we can set cacheDir to our configured location.
//
// Returns helmRepo unchanged when no resolution was possible.
func resolveHTTPChartURL(ctx context.Context, helmRepo, requestedVersion, tmpDir string, providers getter.Providers, entry *helmrepo.Entry) (string, error) {
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
	entry.URL = repoBase

	chartVersion := ref.Tag
	if requestedVersion != "" {
		chartVersion = requestedVersion
	}

	cacheDir, err := os.MkdirTemp(tmpDir, "helm-index*")
	if err != nil {
		return "", fmt.Errorf("error creating temp dir for index.yaml: %w", err)
	}
	defer func() { _ = os.RemoveAll(cacheDir) }()

	chartRepo, err := helmrepo.NewChartRepository(entry, providers)
	if err != nil {
		return "", fmt.Errorf("error creating chart repository for %q: %w", repoBase, err)
	}
	chartRepo.CachePath = cacheDir

	slog.DebugContext(ctx, "fetching Helm repository index", "url", repoBase)

	idxPath, err := chartRepo.DownloadIndexFile()
	if err != nil {
		return "", fmt.Errorf("error fetching index.yaml from %q: %w", repoBase, err)
	}

	index, err := helmrepo.LoadIndexFile(idxPath)
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

// sameHost reports whether two URLs share the same scheme and host (including port).
func sameHost(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host
}
