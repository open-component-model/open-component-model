package catalog

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote/errcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sortedCatalogServer serves the Distribution catalog endpoint over a sorted
// repository list. It honours the n and last query parameters the ORAS client
// sends and sets a Link header while more entries remain, so the test drives
// real cursor pagination.
func sortedCatalogServer(t *testing.T, repos []string, pageSize int) *httptest.Server {
	t.Helper()
	sorted := append([]string(nil), repos...)
	sort.Strings(sorted)

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		last := r.URL.Query().Get("last")
		start := sort.Search(len(sorted), func(i int) bool { return sorted[i] > last })
		end := min(start+pageSize, len(sorted))
		page := sorted[start:end]
		if end < len(sorted) && len(page) > 0 {
			next := fmt.Sprintf("/v2/_catalog?n=%d&last=%s", pageSize, url.QueryEscape(page[len(page)-1]))
			w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"next\"", next))
		}
		writeCatalogPage(t, w, page)
	})
	return httptest.NewServer(mux)
}

func writeCatalogPage(t *testing.T, w http.ResponseWriter, repos []string) {
	t.Helper()
	body, err := json.Marshal(struct {
		Repositories []string `json:"repositories"`
	}{Repositories: repos})
	require.NoError(t, err)
	_, err = w.Write(body)
	require.NoError(t, err)
}

func TestDistributionCataloger_ListComponentsAcrossPages(t *testing.T) {
	// The server sorts these. With a small page size the matching block spans
	// several pages, so the test drives cursor pagination and the early stop
	// once the scan passes the block.
	repos := []string{
		"component-descriptors/componentB",
		"component-descriptors/componentA",
		"component-descriptors/componentD",
		"component-descriptors/componentC",
		"my/sub/component-descriptors/after-the-block",
		"not-a-component",
	}
	srv := sortedCatalogServer(t, repos, 2)
	defer srv.Close()

	cataloger, err := NewDistributionCataloger(CatalogerOptions{
		Host:      strings.TrimPrefix(srv.URL, "http://"),
		PlainHTTP: true,
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, "")

	var result []string
	err = lister.ListComponents(t.Context(), "", func(names []string) error {
		result = append(result, names...)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"componentA", "componentB", "componentC", "componentD"}, result)
}

func TestDistributionCataloger_SubPathStripping(t *testing.T) {
	repos := []string{
		"component-descriptors/componentB",
		"my/sub/component-descriptors/componentA",
		"my/sub/component-descriptors/componentC",
		"other/component-descriptors/after-the-block",
	}
	srv := sortedCatalogServer(t, repos, 2)
	defer srv.Close()

	cataloger, err := NewDistributionCataloger(CatalogerOptions{
		Host:      strings.TrimPrefix(srv.URL, "http://"),
		PlainHTTP: true,
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, "my/sub")

	var result []string
	err = lister.ListComponents(t.Context(), "", func(names []string) error {
		result = append(result, names...)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"componentA", "componentC"}, result)
}

func TestDistributionCataloger_NotFoundMapsToUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cataloger, err := NewDistributionCataloger(CatalogerOptions{
		Host:      strings.TrimPrefix(srv.URL, "http://"),
		PlainHTTP: true,
	})
	require.NoError(t, err)

	err = cataloger.Repositories(t.Context(), "", func(repos []string) error { return nil })
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCatalogUnsupported, "expected ErrCatalogUnsupported, got: %v", err)
}

func TestDistributionCataloger_CallbackErrorIsNotCatalogUnsupported(t *testing.T) {
	srv := sortedCatalogServer(t, []string{"component-descriptors/componentA"}, 100)
	defer srv.Close()

	cataloger, err := NewDistributionCataloger(CatalogerOptions{
		Host:      strings.TrimPrefix(srv.URL, "http://"),
		PlainHTTP: true,
	})
	require.NoError(t, err)

	// A callback that fails with a not-found error must surface as the
	// callback's own error, not as ErrCatalogUnsupported.
	sentinel := errors.New("callback failed")
	err = cataloger.Repositories(t.Context(), "", func(repos []string) error {
		return errors.Join(sentinel, &errcode.ErrorResponse{StatusCode: http.StatusNotFound})
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	assert.NotErrorIs(t, err, ErrCatalogUnsupported)
}

func TestDistributionCataloger_UsesHTTPClientWhenNoAuthClient(t *testing.T) {
	var count int
	srv := sortedCatalogServer(t, []string{"component-descriptors/componentA"}, 100)
	defer srv.Close()

	// A custom client with a counting transport proves the cataloger uses the
	// provided HTTP client rather than the ORAS default when no auth client is
	// given.
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		count++
		return http.DefaultTransport.RoundTrip(req)
	})}

	cataloger, err := NewDistributionCataloger(CatalogerOptions{
		Host:       strings.TrimPrefix(srv.URL, "http://"),
		PlainHTTP:  true,
		HTTPClient: httpClient,
	})
	require.NoError(t, err)

	require.NoError(t, cataloger.Repositories(t.Context(), "", func(repos []string) error { return nil }))
	assert.Positive(t, count, "the provided HTTP client must be used")
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
