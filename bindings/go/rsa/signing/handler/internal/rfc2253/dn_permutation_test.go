package rfc2253_test

import (
	"crypto/x509/pkix"
	"testing"

	"github.com/stretchr/testify/require"
	dn "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/rfc2253"
)

func perms(ss []string) [][]string {
	n := len(ss)
	if n <= 1 {
		return [][]string{append([]string(nil), ss...)}
	}
	var out [][]string
	for i := range ss {
		rest := append(append([]string(nil), ss[:i]...), ss[i+1:]...)
		for _, p := range perms(rest) {
			out = append(out, append([]string{ss[i]}, p...))
		}
	}
	return out
}

func cloneName(n pkix.Name) pkix.Name {
	return pkix.Name{
		CommonName:         n.CommonName,
		Country:            append([]string(nil), n.Country...),
		Organization:       append([]string(nil), n.Organization...),
		OrganizationalUnit: append([]string(nil), n.OrganizationalUnit...),
		Locality:           append([]string(nil), n.Locality...),
		Province:           append([]string(nil), n.Province...),
		StreetAddress:      append([]string(nil), n.StreetAddress...),
		PostalCode:         append([]string(nil), n.PostalCode...),
		SerialNumber:       n.SerialNumber,
		Names:              append([]pkix.AttributeTypeAndValue(nil), n.Names...),
		ExtraNames:         append([]pkix.AttributeTypeAndValue(nil), n.ExtraNames...),
	}
}

func TestMatch_Permutations_Complete_Country(t *testing.T) {
	base := pkix.Name{
		CommonName: "a",
		Country:    []string{"DE", "US"},
	}
	for _, nOrder := range perms(base.Country) {
		for _, pOrder := range perms(base.Country) {
			n := cloneName(base)
			p := cloneName(base)
			n.Country = nOrder
			p.Country = pOrder
			require.NoError(t, dn.Match(n, p),
				"n=%v p=%v", n.Country, p.Country)
		}
	}
}

func TestMatch_Permutations_Subset_Country(t *testing.T) {
	base := pkix.Name{
		CommonName: "a",
		Country:    []string{"DE", "US"},
	}
	sub := []string{"DE"}
	for _, nOrder := range perms(base.Country) {
		n := cloneName(base)
		p := cloneName(base)
		n.Country = nOrder
		p.Country = sub
		require.NoError(t, dn.Match(n, p),
			"n=%v p=%v", n.Country, p.Country)
	}
}

func TestMatch_Permutations_MultipleFields(t *testing.T) {
	base := pkix.Name{
		CommonName:         "a",
		Country:            []string{"DE", "US"},
		Organization:       []string{"OrgB", "OrgA"},
		OrganizationalUnit: []string{"OU2", "OU1"},
	}

	for _, c := range perms(base.Country) {
		for _, o := range perms(base.Organization) {
			for _, ou := range perms(base.OrganizationalUnit) {
				n := cloneName(base)
				p := cloneName(base)

				n.Country, p.Country = c, c
				n.Organization, p.Organization = o, o
				n.OrganizationalUnit, p.OrganizationalUnit = ou, ou

				require.NoError(t, dn.Match(n, p),
					"country=%v org=%v ou=%v", c, o, ou)
			}
		}
	}
}

func TestMatch_Permutations_MultiField_Subsets(t *testing.T) {
	base := pkix.Name{
		CommonName:         "a",
		Country:            []string{"DE", "US"},
		Organization:       []string{"OrgA", "OrgB"},
		OrganizationalUnit: []string{"OU1", "OU2"},
	}
	pattern := pkix.Name{
		CommonName:         "a",
		Country:            []string{"DE"},
		Organization:       []string{"OrgA"},
		OrganizationalUnit: []string{"OU2"},
	}

	for _, c := range perms(base.Country) {
		for _, o := range perms(base.Organization) {
			for _, ou := range perms(base.OrganizationalUnit) {
				n := cloneName(base)
				n.Country = c
				n.Organization = o
				n.OrganizationalUnit = ou

				require.NoError(t, dn.Match(n, pattern),
					"n.country=%v n.org=%v n.ou=%v p=%v", c, o, ou, pattern)
			}
		}
	}
}
