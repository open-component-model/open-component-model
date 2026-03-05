package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/downloader"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
)

func NewReadOnlyChartFromRemote(ctx context.Context, helmRepo string, opts ...Option) (result *helm.ChartData, err error) {
	opt := &option{}
	for _, o := range opts {
		o(opt)
	}

	if opt.TempDir == "" {
		opt.TempDir = os.TempDir()
	}

	if opt.Credentials == nil {
		opt.Credentials = make(map[string]string)
	}

	// Since this temporary folder is created with tmpDirBase as a prefix, it will be cleaned up by the caller.
	tmpDir, err := os.MkdirTemp(opt.TempDir, "helmRemoteChart*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory: %w", err)
	}

	var getterOpts []getter.Option
	tlsOption, err := constructTLSOptions(opt)
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

	if v, ok := opt.Credentials[CredentialKeyring]; ok {
		keyring = v
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

	// TODO(fabianburth): check whether this needs additional configuration
	regClient, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("error creating registry client: %w", err)
	}

	dl := &downloader.ChartDownloader{
		Out:     os.Stderr,
		Verify:  verify,
		Getters: getterProviders(),
		// set by ocm v1 originally.
		RepositoryCache:  filepath.Join(opt.TempDir, ".helmcache"),
		RepositoryConfig: filepath.Join(opt.TempDir, ".helmrepo"),
		ContentCache:     filepath.Join(opt.TempDir, ".helmcontent"),
		RegistryClient:   regClient,
		Options:          getterOpts,
		Keyring:          keyring,
	}

	if username, ok := opt.Credentials[ocicredentials.CredentialKeyUsername]; ok {
		if password, ok := opt.Credentials[ocicredentials.CredentialKeyPassword]; ok {
			dl.Options = append(dl.Options, getter.WithBasicAuth(username, password))
		}
	}

	version, err := getVersion(opt.Version, helmRepo)
	if err != nil {
		return nil, fmt.Errorf("error determining chart version: %w", err)
	}

	savedPath, _, err := dl.DownloadTo(helmRepo, version, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("error downloading chart %q version %q: %w", helmRepo, version, err)
	}

	chart, err := loader.Load(savedPath)
	if err != nil {
		return nil, fmt.Errorf("error loading downloaded chart from %q: %w", savedPath, err)
	}

	result = &helm.ChartData{
		Name:         chart.Name(),
		Version:      chart.Metadata.Version,
		ChartTempDir: tmpDir,
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

// constructTLSOptions sets up the TLS configuration files based on the helm specification
func constructTLSOptions(opts *option) (_ getter.Option, err error) {
	var (
		caFile                        *os.File
		caFilePath, certFile, keyFile string
		credentials                   = opts.Credentials
		tmpDir                        = opts.TempDir
	)

	if credentials == nil {
		credentials = make(map[string]string)
	}

	if opts.CACertFile != "" {
		caFilePath = opts.CACertFile
	} else if opts.CACert != "" {
		caFile, err = os.CreateTemp(tmpDir, "caCert-*.pem")
		if err != nil {
			return nil, fmt.Errorf("error creating temporary CA certificate file: %w", err)
		}
		defer func() {
			if cerr := caFile.Close(); cerr != nil {
				err = errors.Join(err, cerr)
			}
		}()
		if _, err = caFile.WriteString(opts.CACert); err != nil {
			return nil, fmt.Errorf("error writing CA certificate to temp file: %w", err)
		}
		caFilePath = caFile.Name()
	}

	// set up certFile and keyFile if they are provided in the credentials
	if v, ok := credentials[CredentialCertFile]; ok {
		certFile = v
		if _, err := os.Stat(certFile); err != nil {
			return nil, fmt.Errorf("certFile %q does not exist", certFile)
		}
	}

	if v, ok := credentials[CredentialKeyFile]; ok {
		keyFile = v
		if _, err := os.Stat(keyFile); err != nil {
			return nil, fmt.Errorf("keyFile %q does not exist", keyFile)
		}
	}

	// it's safe to always add this option even with empty values
	// because the default is empty.
	return getter.WithTLSClientConfig(certFile, keyFile, caFilePath), nil
}

// getterProviders returns the available getter providers.
// This replaces the need for cli.New() and avoids the explosion of the dependency tree.
func getterProviders() getter.Providers {
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
