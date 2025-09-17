// dn_match_test.go
package rfc2253_test

import (
	"crypto/x509/pkix"
	"testing"

	"github.com/stretchr/testify/require"
	dn "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/rfc2253"
)

func TestMatch_Complete(t *testing.T) {
	n := pkix.Name{
		CommonName: "a",
		Country:    []string{"DE", "US"},
	}
	require.NoError(t, dn.Match(n, n))
}

func TestMatch_Partly_NoCountryInPattern(t *testing.T) {
	n := pkix.Name{
		CommonName: "a",
		Country:    []string{"DE", "US"},
	}
	p := n
	p.Country = nil
	require.NoError(t, dn.Match(n, p))
}

func TestMatch_Partly_SubsetList(t *testing.T) {
	n := pkix.Name{
		CommonName: "a",
		Country:    []string{"DE", "US"},
	}
	p := n
	p.Country = []string{"DE"}
	require.NoError(t, dn.Match(n, p))
}

func TestMatch_FailsForMissing(t *testing.T) {
	n := pkix.Name{
		CommonName: "a",
		Country:    []string{"DE", "US"},
	}
	p := n
	p.Country = []string{"EG"}

	err := dn.Match(n, p)
	require.Error(t, err)
	// matches current containsAll error format in your code
	require.EqualError(t, err, `country [DE US] does not include required [EG]`)
}

func TestMatch_CommonNameMismatch(t *testing.T) {
	n := pkix.Name{CommonName: "alice"}
	p := pkix.Name{CommonName: "bob"}
	err := dn.Match(n, p)
	require.EqualError(t, err, `common name "alice" does not match "bob"`)
}
