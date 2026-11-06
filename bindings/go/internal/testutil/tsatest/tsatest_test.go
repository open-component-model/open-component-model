package tsatest_test

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/digitorus/timestamp"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/tsatest"
)

func TestStart(t *testing.T) {
	fixed := time.Now().Add(-48 * time.Hour)
	tests := []struct {
		name      string
		opts      []tsatest.Option
		withCerts bool
		wantTime  func(t *testing.T, got time.Time)
	}{
		{
			name:      "request time, certificate embedded",
			withCerts: true,
			wantTime: func(t *testing.T, got time.Time) {
				t.Helper()
				require.WithinDuration(t, time.Now(), got, 5*time.Second)
			},
		},
		{
			name: "fixed time, certificate not requested",
			opts: []tsatest.Option{tsatest.WithTime(fixed)},
			wantTime: func(t *testing.T, got time.Time) {
				t.Helper()
				require.True(t, fixed.UTC().Truncate(time.Second).Equal(got))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			tsa := tsatest.Start(t, tt.opts...)
			digest := sha256.Sum256([]byte("payload"))
			reqBody, err := timestamp.CreateRequest(bytes.NewReader([]byte("payload")), &timestamp.RequestOptions{
				Hash:         crypto.SHA256,
				Certificates: tt.withCerts,
			})
			r.NoError(err)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, tsa.URL, bytes.NewReader(reqBody))
			r.NoError(err)
			req.Header.Set("Content-Type", "application/timestamp-query")
			resp, err := http.DefaultClient.Do(req)
			r.NoError(err)
			defer func() { _ = resp.Body.Close() }()
			r.Equal(http.StatusOK, resp.StatusCode)
			respBody, err := io.ReadAll(resp.Body)
			r.NoError(err)

			ts, err := timestamp.ParseResponse(respBody)
			r.NoError(err)
			r.Equal(digest[:], ts.HashedMessage)
			tt.wantTime(t, ts.Time)
			r.Equal(int64(1), tsa.Requests())
			if tt.withCerts {
				r.Len(ts.Certificates, 1)
				r.True(tsa.Cert.Equal(ts.Certificates[0]))
			} else {
				r.Empty(ts.Certificates)
			}
			r.True(tsa.Cert.NotBefore.Before(ts.Time), "the certificate must cover the attested time")
			r.Equal([]x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}, tsa.Cert.ExtKeyUsage)
		})
	}
}
