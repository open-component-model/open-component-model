// Package fips140 runs Helm chart downloads with GODEBUG=fips140=only.
//
// Helm verifies chart provenance with OpenPGP outside the Go Cryptographic
// Module, which panics in strict FIPS 140-3 mode. OCM rejects provenance
// verification in that mode with an error instead. The mode is fixed per
// process, so the directive below applies to this package's test binary only.
//go:debug fips140=only
package fips140

import (
	"crypto/fips140"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/helm/internal/download"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
)

func TestProvenanceVerificationRejected(t *testing.T) {
	r := require.New(t)
	r.True(fips140.Enforced(), "test binary must run with fips140=only")

	_, err := download.NewReadOnlyChartFromRemote(t.Context(), "https://charts.example.invalid/chart-1.0.0.tgz", t.TempDir(),
		download.WithCredentials(&helmcredsv1.HelmHTTPCredentials{Keyring: "/keyring.gpg"}))
	r.ErrorIs(err, download.ErrProvenanceVerificationInFIPSMode)
}
