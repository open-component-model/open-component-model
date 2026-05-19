package input

import (
	"context"
	"errors"
	"fmt"
	"os"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm/internal"
	dlinternal "ocm.software/open-component-model/bindings/go/helm/internal/download"
	"ocm.software/open-component-model/bindings/go/helm/internal/oci"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

const (
	HelmRepositoryType = "helmChart"
)

// ReadOnlyChart contains Helm chart contents as tgz archive, some metadata and optionally a provenance file.
type ReadOnlyChart struct {
	Name      string
	Version   string
	ChartBlob *filesystem.Blob
	ProvBlob  *filesystem.Blob
}

// Option is a function that modifies Options.
type Option func(options *Options)

// WithCredentials sets the credentials to use for the remote repository.
func WithCredentials(credentials *helmcredsv1.HelmHTTPCredentials) Option {
	return func(options *Options) {
		options.Credentials = credentials
	}
}

// WithOCICredentials sets the credentials to use for an oci registry.
func WithOCICredentials(credentials *ocicredsv1.OCICredentials) Option {
	return func(options *Options) {
		options.OCICredentials = credentials
	}
}

type Options struct {
	Credentials    *helmcredsv1.HelmHTTPCredentials
	OCICredentials *ocicredsv1.OCICredentials
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
		chart, err = newReadOnlyChartFromRemote(ctx, helmSpec, tmpDir, options.Credentials, options.OCICredentials)
		if err != nil {
			return nil, nil, fmt.Errorf("error loading remote helm chart from %q: %w", helmSpec.HelmRepository, err)
		}
	default:
		return nil, nil, fmt.Errorf("either path or helmRepository must be specified")
	}

	result, err := oci.CopyChartToOCILayout(ctx, &internal.ChartData{
		Name:      chart.Name,
		Version:   chart.Version,
		ChartBlob: chart.ChartBlob,
		ProvBlob:  chart.ProvBlob,
	}, tmpDir)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting helm chart to OCI layout: %w", err)
	}

	return result.Blob, chart, nil
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
func newReadOnlyChartFromRemote(
	ctx context.Context,
	helmSpec v1.Helm,
	tmpDirBase string,
	credentials *helmcredsv1.HelmHTTPCredentials,
	ociCredentials *ocicredsv1.OCICredentials,
) (result *ReadOnlyChart, err error) {
	opts := []dlinternal.Option{
		dlinternal.WithCredentials(credentials),
		dlinternal.WithOCICredentials(ociCredentials),
		//nolint:staticcheck // downward compatibility for helm input
		dlinternal.WithVersion(helmSpec.Version),
		//nolint:staticcheck // downward compatibility for helm input
		dlinternal.WithCACert(helmSpec.CACert),
		//nolint:staticcheck // downward compatibility for helm input
		dlinternal.WithCACertFile(helmSpec.CACertFile),
	}
	resultChart, err := dlinternal.NewReadOnlyChartFromRemote(ctx, helmSpec.HelmRepository, tmpDirBase, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading helm chart from repository %q: %w", helmSpec.HelmRepository, err)
	}

	return &ReadOnlyChart{
		Name:      resultChart.Name,
		Version:   resultChart.Version,
		ChartBlob: resultChart.ChartBlob,
		ProvBlob:  resultChart.ProvBlob,
	}, err
}
