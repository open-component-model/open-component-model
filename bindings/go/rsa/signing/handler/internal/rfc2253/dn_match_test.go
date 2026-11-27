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

func TestMatch_ExtraNamesAgainstParsedCertificateNames(t *testing.T) {
	email := pkix.AttributeTypeAndValue{Type: []int{1, 2, 840, 113549, 1, 9, 1}, Value: "a@b.c"}
	// A name parsed from a certificate carries non-standard attributes in Names only.
	cert := pkix.Name{
		CommonName: "x",
		Names: []pkix.AttributeTypeAndValue{
			{Type: []int{2, 5, 4, 3}, Value: "x"},
			email,
		},
	}
	p, err := dn.Parse("CN=x,1.2.840.113549.1.9.1=a@b.c")
	require.NoError(t, err)
	require.NoError(t, dn.Equal(p, cert))

	other, err := dn.Parse("CN=x,1.2.840.113549.1.9.1=other@b.c")
	require.NoError(t, err)
	require.ErrorContains(t, dn.Match(cert, other), "missing extra attribute")

	bare, err := dn.Parse("CN=x")
	require.NoError(t, err)
	require.NoError(t, dn.Match(bare, cert))
}

func TestEqual_CertificateExtraAttributesAreSymmetric(t *testing.T) {
	cert := pkix.Name{
		CommonName: "x",
		Names: []pkix.AttributeTypeAndValue{
			{Type: []int{2, 5, 4, 3}, Value: "x"},
			{Type: []int{1, 2, 840, 113549, 1, 9, 1}, Value: "a@b.c"},
		},
	}
	p, err := dn.Parse("CN=x")
	require.NoError(t, err)
	require.ErrorContains(t, dn.Equal(p, cert), "missing extra attribute")
	require.ErrorContains(t, dn.Equal(cert, p), "missing extra attribute")
}
