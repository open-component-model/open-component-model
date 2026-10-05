// Package fips140 runs the wget resource repository with GODEBUG=fips140=only.
//
// Servers advertise MD5 and SHA-1 checksums, which strict FIPS 140-3 mode
// rejects. OCM still verifies them (checksum.Algorithm.New lifts enforcement for
// these two), and these tests catch a path that hashes with them directly. The
// mode is fixed per process, so the directive below applies to this package's
// test binary only.
//go:debug fips140=only
package fips140

import (
	"crypto/fips140"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/repository"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

func TestDownloadResource_LegacyChecksums(t *testing.T) {
	require.True(t, fips140.Enforced(), "test binary must run with fips140=only")

	content := []byte("fips140=only checksum")
	tests := []struct {
		name, header, value, wantErr string
	}{
		{name: "MD5 verified", header: "x-checksum-md5", value: "97ecb023e4fef4fba2158fb39c57736a"},
		{name: "SHA-1 verified", header: "x-checksum-sha1", value: "5c8a44d96c0088e3e370b597f7f8bfbd2eeca598"},
		{name: "MD5 mismatch rejected", header: "x-checksum-md5", value: strings.Repeat("0", 32), wantErr: "checksum verification failed"},
		{name: "SHA-1 mismatch rejected", header: "x-checksum-sha1", value: strings.Repeat("0", 40), wantErr: "checksum verification failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(tt.header, tt.value)
				_, _ = w.Write(content)
			}))
			t.Cleanup(server.Close)

			repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()),
				repository.WithChecksumConfig(&checksumhttpv1alpha1.Config{Mode: checksumhttpv1alpha1.ChecksumModeRequire}))
			b, err := repo.DownloadResource(t.Context(), wgetResource(t, server.URL+"/resource"), nil)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			rc, err := b.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { _ = rc.Close() })
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(content, data)
		})
	}
}

func wgetResource(t *testing.T, url string) *descruntime.Resource {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"url": url})
	require.NoError(t, err)
	res := &descruntime.Resource{}
	res.Name, res.Version, res.Type = "fips-resource", "1.0.0", "blob"
	res.Access = &runtime.Raw{Type: runtime.NewVersionedType("wget", v1.Version), Data: raw}
	return res
}
