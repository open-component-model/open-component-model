package input

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

const (
	HelmRepositoryType = "helmChart"
	// CredentialCertFile is the key for storing the location of a client certificate.
	CredentialCertFile = "certFile"
	// CredentialKeyFile is the key for storing the location of a client private key.
	CredentialKeyFile = "keyFile"
	// CredentialKeyring is the key for storing the keyring name to use.
	CredentialKeyring = "keyring"
)

const (
	// DefaultHTTPTimeout
	// The cost timeout references curl's default connection timeout.
	// https://github.com/curl/curl/blob/master/lib/connect.h#L40C21-L40C21
	// The helm commands are usually executed manually. Considering the acceptable waiting time, we reduced the entire request time to 120s.
	DefaultHTTPTimeout = 120
)

var defaultOptions = []getter.Option{getter.WithTimeout(time.Second * DefaultHTTPTimeout)}

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

// ReadOnlyChart contains Helm chart contents as tgz archive, some metadata and optionally a provenance file.
type ReadOnlyChart struct {
	Name      string
	Version   string
	ChartBlob *filesystem.Blob
	ProvBlob  *filesystem.Blob

	// chartTempDir is the temporary directory where the chart is downloaded to. This is cleaned after the writer
	// has finished with copying it later in copyChartToOCILayoutAsync.
	chartTempDir string
}

// Option is a function that modifies Options.
type Option func(options *Options)

// WithCredentials sets the credentials to use for the remote repository.
// The credentials could contain the following keys:
// - "username": for basic authentication
// - "password": for basic authentication
// - "certFile": for TLS client certificate
// - "keyFile": for TLS client private key
// - "keyring": for keyring name to use
// - "caCert": for CA certificate
// - "caCertFile": for CA certificate file
func WithCredentials(credentials map[string]string) Option {
	return func(options *Options) {
		options.Credentials = credentials
	}
}

type Options struct {
	Credentials map[string]string
}

// GetV1HelmBlob creates a ReadOnlyBlob from a v1.Helm specification.
// It reads the contents from the filesystem or downloads from a remote repository,
// then packages it as an OCI artifact. The function returns an error if neither path
// nor helmRepository are specified, or if there are issues reading/downloading the chart.
func GetV1HelmBlob(ctx context.Context, helmSpec v1.Helm, tmpDir string, opts ...Option) (blob.ReadOnlyBlob, *ReadOnlyChart, error) {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	if err := validateInputSpec(helmSpec); err != nil {
		return nil, nil, fmt.Errorf("invalid helm input spec: %w", err)
	}

	var chart *ReadOnlyChart
	var err error

	switch {
	case helmSpec.Path != "":
		chart, err = newReadOnlyChart(helmSpec.Path, tmpDir)
		if err != nil {
			return nil, nil, fmt.Errorf("error loading local helm chart %q: %w", helmSpec.Path, err)
		}
	case helmSpec.HelmRepository != "":
		chart, err = newReadOnlyChartFromRemote(ctx, helmSpec, tmpDir, options.Credentials)
		if err != nil {
			return nil, nil, fmt.Errorf("error loading remote helm chart from %q: %w", helmSpec.HelmRepository, err)
		}
	default:
		return nil, nil, fmt.Errorf("either path or helmRepository must be specified")
	}

	b := copyChartToOCILayout(ctx, chart)

	return b, chart, nil
}

func validateInputSpec(helmSpec v1.Helm) error {
	var err error

	if helmSpec.Path == "" && helmSpec.HelmRepository == "" {
		err = errors.New("either path or helmRepository must be specified")
	}

	if helmSpec.Path != "" && helmSpec.HelmRepository != "" {
		err = errors.New("only one of path or helmRepository can be specified")
	}

	return err
}

func newReadOnlyChart(path, tmpDirBase string) (result *ReadOnlyChart, err error) {
	// Load the chart from filesystem, the path can be either a helm chart directory or a tgz file.
	// While loading the chart is also validated.
	chart, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("error loading helm chart from path %q: %w", path, err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("error retrieving file information for %q: %w", path, err)
	}

	result = &ReadOnlyChart{
		Name:    chart.Name(),
		Version: chart.Metadata.Version,
	}

	if fi.IsDir() {
		// If path is a directory, we need to create a tgz archive in a temporary folder.
		tmpDir, err := os.MkdirTemp(tmpDirBase, "chartDirToTgz*")
		if err != nil {
			return nil, fmt.Errorf("error creating temporary directory")
		}

		// Save the chart as a tgz archive. If the directory is /foo, and the chart is named bar, with version 1.0.0,
		// this will generate /foo/bar-1.0.0.tgz
		// TODO: contribution to Helm to allow to write to tar in memory instead of a file.
		path, err = chartutil.Save(chart, tmpDir)
		if err != nil {
			return nil, fmt.Errorf("error saving archived chart to directory %q: %w", tmpDir, err)
		}
	}

	// Now we know that the path refers to a valid Helm chart packaged as a tgz file. Thus returning its contents.
	if result.ChartBlob, err = filesystem.GetBlobFromOSPath(path); err != nil {
		return nil, fmt.Errorf("error creating blob from file %q: %w", path, err)
	}

	provName := path + ".prov" // foo.prov
	if _, err := os.Stat(provName); err == nil {
		if result.ProvBlob, err = filesystem.GetBlobFromOSPath(provName); err != nil {
			return nil, fmt.Errorf("error creating blob from file %q: %w", path, err)
		}
	}

	return result, nil
}

// newReadOnlyChartFromRemote downloads a chart from a remote Helm repository
// and creates a ReadOnlyChart from it.
func newReadOnlyChartFromRemote(ctx context.Context, helmSpec v1.Helm, tmpDirBase string, credentials map[string]string) (result *ReadOnlyChart, err error) {
	// Since this temporary folder is created with tmpDirBase as a prefix, it will be cleaned up by the caller.
	tmpDir, err := os.MkdirTemp(tmpDirBase, "helmRemoteChart*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory: %w", err)
	}

	var opts []getter.Option
	tlsOption, err := constructTLSOptions(helmSpec, tmpDir, credentials)
	if err != nil {
		return nil, fmt.Errorf("error setting up TLS options: %w", err)
	}
	opts = append(opts, tlsOption)

	var (
		keyring string
		verify  = downloader.VerifyNever
	)
	if v, ok := credentials[CredentialKeyring]; ok {
		keyring = v
		// We set verifyIfPossible to allow the download to run verify if keyring is defined. Without the keyring
		// verification would not be possible at all.
		// https://github.com/open-component-model/ocm/blob/be847549af3d2947a2c8bc2b38d51a20c2a8a9ba/api/tech/helm/downloader.go#L128
		verify = downloader.VerifyIfPossible
	}

	var plainHTTP bool
	if strings.HasPrefix(helmSpec.HelmRepository, "http://") {
		slog.WarnContext(ctx, "using plain HTTP for chart download",
			"repository", helmSpec.HelmRepository,
		)
		plainHTTP = true
	}

	opts = append(opts, getter.WithPlainHTTP(plainHTTP))

	dl := &downloader.ChartDownloader{
		Out:     os.Stderr,
		Verify:  verify,
		Getters: getterProviders(),
		// set by ocm v1 originally.
		RepositoryCache:  "/tmp/.helmcache",
		RepositoryConfig: "/tmp/.helmrepo",
		Options:          opts,
		Keyring:          keyring,
	}

	if username, ok := credentials[ocicredentials.CredentialKeyUsername]; ok {
		if password, ok := credentials[ocicredentials.CredentialKeyPassword]; ok {
			dl.Options = append(dl.Options, getter.WithBasicAuth(username, password))
		}
	}

	// We don't let helm download decide on the version of the chart. Version, either through ref or through
	// the spec.Version field always MUST be defined. This is only true for OCI repositories.
	// In the case of HTTP/S repositories, the version is taken from the URL.
	version := helmSpec.Version
	if version == "" && strings.HasPrefix(helmSpec.HelmRepository, "oci://") {
		stripped := strings.TrimPrefix(helmSpec.HelmRepository, "oci://")
		ref, err := looseref.ParseReference(stripped)
		if err != nil {
			return nil, fmt.Errorf("error parsing helm repository reference %q: %w", helmSpec.HelmRepository, err)
		}

		if ref.Tag == "" {
			return nil, errors.New("either helm repository tag or spec.Version has to be set")
		}

		version = ref.Tag
	}

	savedPath, _, err := dl.DownloadTo(helmSpec.HelmRepository, version, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("error downloading chart %q version %q: %w", helmSpec.HelmRepository, helmSpec.Version, err)
	}

	chart, err := loader.Load(savedPath)
	if err != nil {
		return nil, fmt.Errorf("error loading downloaded chart from %q: %w", savedPath, err)
	}

	result = &ReadOnlyChart{
		Name:         chart.Name(),
		Version:      chart.Metadata.Version,
		chartTempDir: tmpDir,
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
func constructTLSOptions(helmSpec v1.Helm, tmpDir string, credentials map[string]string) (_ getter.Option, err error) {
	var (
		caFile                        *os.File
		caFilePath, certFile, keyFile string
	)

	if helmSpec.CACertFile != "" {
		caFilePath = helmSpec.CACertFile
	} else if helmSpec.CACert != "" {
		caFile, err = os.CreateTemp(tmpDir, "caCert-*.pem")
		if err != nil {
			return nil, fmt.Errorf("error creating temporary CA certificate file: %w", err)
		}
		defer func() {
			if cerr := caFile.Close(); cerr != nil {
				err = errors.Join(err, cerr)
			}
		}()
		if _, err = caFile.WriteString(helmSpec.CACert); err != nil {
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

// copyChartToOCILayout takes a ReadOnlyChart helper object and creates an OCI layout from it.
// Three OCI layers are expected: config, tgz contents and optionally a provenance file.
// The result is tagged with the helm chart version.
// See also: https://github.com/helm/community/blob/main/hips/hip-0006.md#2-support-for-provenance-files
func copyChartToOCILayout(ctx context.Context, chart *ReadOnlyChart) *direct.Blob {
	r, w := io.Pipe()

	go copyChartToOCILayoutAsync(ctx, chart, w)

	// TODO(ikhandamirov): replace this with a direct/unbuffered blob.
	return direct.New(r, direct.WithMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1))
}

func copyChartToOCILayoutAsync(ctx context.Context, chart *ReadOnlyChart, w *io.PipeWriter) {
	// err accumulates any error from copy, gzip, or layout writing.
	var err error
	defer func() {
		_ = w.CloseWithError(err)            // Always returns nil.
		_ = os.RemoveAll(chart.chartTempDir) // Always remove the created temp folder for the chart.
	}()

	zippedBuf := gzip.NewWriter(w)
	defer func() {
		err = errors.Join(err, zippedBuf.Close())
	}()

	// Create an OCI layout writer over the gzip stream.
	target := tar.NewOCILayoutWriter(zippedBuf)
	defer func() {
		err = errors.Join(err, target.Close())
	}()

	// Generate and Push layers based on the chart to the OCI layout.
	configLayer, chartLayer, provLayer, err := pushChartAndGenerateLayers(ctx, chart, target)
	if err != nil {
		err = fmt.Errorf("failed to push chart layers: %w", err)
		return
	}

	layers := []ociImageSpecV1.Descriptor{*chartLayer}
	if provLayer != nil {
		// If a provenance file was provided, add it to the layers.
		layers = append(layers, *provLayer)
	}

	// Create OCI image manifest.
	imgDesc, perr := oras.PackManifest(ctx, target, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		ConfigDescriptor: configLayer,
		Layers:           layers,
	})
	if perr != nil {
		err = fmt.Errorf("failed to create OCI image manifest: %w", perr)
		return
	}

	if terr := target.Tag(ctx, imgDesc, chart.Version); terr != nil {
		err = fmt.Errorf("failed to tag OCI image: %w", terr)
		return
	}
}

func pushChartAndGenerateLayers(ctx context.Context, chart *ReadOnlyChart, target oras.Target) (
	configLayer *ociImageSpecV1.Descriptor,
	chartLayer *ociImageSpecV1.Descriptor,
	provLayer *ociImageSpecV1.Descriptor,
	err error,
) {
	// Create config OCI layer.
	if configLayer, err = pushConfigLayer(ctx, chart.Name, chart.Version, target); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create and push helm chart config layer: %w", err)
	}

	// Create Helm Chart OCI layer.
	if chartLayer, err = pushChartLayer(ctx, chart.ChartBlob, target); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create and push helm chart content layer: %w", err)
	}

	// Create Provenance OCI layer (optional).
	if chart.ProvBlob != nil {
		if provLayer, err = pushProvenanceLayer(ctx, chart.ProvBlob, target); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create and push helm chart provenance: %w", err)
		}
	}
	return configLayer, chartLayer, provLayer, err
}

func pushConfigLayer(ctx context.Context, name, version string, target oras.Target) (_ *ociImageSpecV1.Descriptor, err error) {
	configContent := fmt.Sprintf(`{"name": "%s", "version": "%s"}`, name, version)
	configLayer := &ociImageSpecV1.Descriptor{
		MediaType: registry.ConfigMediaType,
		Digest:    digest.FromString(configContent),
		Size:      int64(len(configContent)),
	}
	if err = target.Push(ctx, *configLayer, strings.NewReader(configContent)); err != nil {
		return nil, fmt.Errorf("failed to push helm chart config layer: %w", err)
	}
	return configLayer, nil
}

func pushProvenanceLayer(ctx context.Context, provenance *filesystem.Blob, target oras.Target) (_ *ociImageSpecV1.Descriptor, err error) {
	provDigStr, known := provenance.Digest()
	if !known {
		return nil, fmt.Errorf("unknown digest for helm provenance")
	}
	provenanceReader, err := provenance.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get a reader for helm chart provenance: %w", err)
	}
	defer func() {
		err = errors.Join(err, provenanceReader.Close())
	}()
	provenanceLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ProvLayerMediaType,
		Digest:    digest.Digest(provDigStr),
		Size:      provenance.Size(),
	}
	if err = target.Push(ctx, provenanceLayer, provenanceReader); err != nil {
		return nil, fmt.Errorf("failed to push helm chart content layer: %w", err)
	}

	return &provenanceLayer, nil
}

func pushChartLayer(ctx context.Context, chart *filesystem.Blob, target oras.Target) (_ *ociImageSpecV1.Descriptor, err error) {
	// We get the reader first because Digest only returns a boolean and no error.
	// This hides errors like, "file not found" or "permission denied" on downloaded content.
	chartReader, err := chart.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get a reader for helm chart blob: %w", err)
	}

	chartDigStr, known := chart.Digest()
	if !known {
		return nil, fmt.Errorf("unknown digest for helm chart")
	}

	chartLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ChartLayerMediaType,
		Digest:    digest.Digest(chartDigStr),
		Size:      chart.Size(),
	}
	defer func() {
		err = errors.Join(err, chartReader.Close())
	}()
	if err = target.Push(ctx, chartLayer, chartReader); err != nil {
		return nil, fmt.Errorf("failed to push helm chart content layer: %w", err)
	}

	return &chartLayer, nil
}
