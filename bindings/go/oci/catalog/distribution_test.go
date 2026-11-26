package catalog

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogServer serves the Distribution catalog endpoint with two pages. The
// first page carries a Link header that points at the second page.
func catalogServer(t *testing.T, page1, page2 []string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/_catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("last") == "" {
			// First page points at the second via the Link header.
			next := fmt.Sprintf("/v2/_catalog?n=%s&last=%s",
				r.URL.Query().Get("n"), url.QueryEscape(page1[len(page1)-1]))
			w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"next\"", next))
			writeCatalogPage(t, w, page1)
			return
		}
		writeCatalogPage(t, w, page2)
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
	page1 := []string{
		"component-descriptors/componentB",
		"component-descriptors/componentA",
		"component-descriptors/componentA",
		"not-a-component",
	}
	page2 := []string{
		"my/sub/component-descriptors/skipped-without-subpath",
		"component-descriptors/componentD",
		"component-descriptors/componentC",
	}
	srv := catalogServer(t, page1, page2)
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
	// Dedup and sort are per page, so the two pages concatenate in order.
	assert.Equal(t, []string{"componentA", "componentB", "componentC", "componentD"}, result)
}

func TestDistributionCataloger_SubPathStripping(t *testing.T) {
	page1 := []string{
		"my/sub/component-descriptors/componentA",
		"component-descriptors/componentB",
	}
	page2 := []string{
		"my/sub/component-descriptors/componentC",
	}
	srv := catalogServer(t, page1, page2)
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
