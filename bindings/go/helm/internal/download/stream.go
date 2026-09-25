package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	helmrepo "helm.sh/helm/v4/pkg/repo/v1"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

// ErrNotStreamable reports a chart reference or credential setup that needs the full helm
// downloader (OCI references, client certificates, custom CAs or provenance verification with
// a keyring), so the chart cannot be streamed with OpenHTTPChart.
var ErrNotStreamable = errors.New("helm chart cannot be streamed directly")

// OpenHTTPChart streams the packaged chart (.tgz) of an HTTP/S helm repository reference
// (a direct .tgz URL or the <repo>/<chart>:<version> form of (*v1.Helm).ChartReference())
// without writing it to disk: the chart URL is resolved via the repository's index.yaml and
// the response body is returned as is. size is the response content length, or -1 when unknown.
// The provenance file is not fetched.
func OpenHTTPChart(ctx context.Context, helmRepo string, opts ...Option) (rc io.ReadCloser, size int64, err error) {
	opt := &option{}
	for _, o := range opts {
		o(opt)
	}
	if !strings.HasPrefix(helmRepo, "http://") && !strings.HasPrefix(helmRepo, "https://") {
		return nil, 0, ErrNotStreamable
	}
	if opt.CACert != "" || opt.CACertFile != "" {
		return nil, 0, ErrNotStreamable
	}
	var username, password string
	if c := opt.Credentials; c != nil {
		if c.CertFile != "" || c.KeyFile != "" || c.Keyring != "" {
			return nil, 0, ErrNotStreamable
		}
		username, password = c.Username, c.Password
	}

	client := ocmhttp.New(ocmhttp.WithConfig(opt.HTTPConfig))
	cfgOpts := HTTPConfigGetterOpts{username: username, password: password, baseURL: helmRepo}
	// index.yaml is repository metadata, not chart content; helm's repository API reads it from
	// a (removed-after-use) cache directory under the system temp dir.
	chartURL, err := resolveHTTPChartURL(ctx, helmRepo, opt.Version, "", GetterProviders(client, cfgOpts), &helmrepo.Entry{
		Name:     "index",
		Username: username,
		Password: password,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("error resolving chart URL %q via index.yaml: %w", helmRepo, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, chartURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/gzip,application/octet-stream")
	// Like helm, only send the repository credentials to the repository's own host.
	if username != "" && password != "" && sameHost(helmRepo, chartURL) {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error downloading chart from %s: %w", redact(chartURL), err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, 0, fmt.Errorf("error downloading chart from %s: %s", redact(chartURL), resp.Status)
	}
	return resp.Body, resp.ContentLength, nil
}

func redact(rawURL string) string {
	if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
		return rawURL[:i]
	}
	return rawURL
}
