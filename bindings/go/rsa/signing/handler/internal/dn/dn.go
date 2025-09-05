// Package dn provides utilities for parsing and matching X.509 Distinguished Names (DNs).
package dn

import (
	"crypto/x509/pkix"
	"fmt"
	"slices"
	"strings"
)

// attributes maps DN attribute keys to functions that update a pkix.Name.
var attributes = map[string]func(*pkix.Name, string){
	"C":          func(n *pkix.Name, v string) { n.Country = append(n.Country, v) },
	"O":          func(n *pkix.Name, v string) { n.Organization = append(n.Organization, v) },
	"OU":         func(n *pkix.Name, v string) { n.OrganizationalUnit = append(n.OrganizationalUnit, v) },
	"L":          func(n *pkix.Name, v string) { n.Locality = append(n.Locality, v) },
	"ST":         func(n *pkix.Name, v string) { n.Province = append(n.Province, v) },
	"STREET":     func(n *pkix.Name, v string) { n.StreetAddress = append(n.StreetAddress, v) },
	"POSTALCODE": func(n *pkix.Name, v string) { n.PostalCode = append(n.PostalCode, v) },
	"SN":         func(n *pkix.Name, v string) { n.SerialNumber = v },
	"CN":         func(n *pkix.Name, v string) { n.CommonName = v },
}

// dnSplitRunes defines characters used to split DN components.
var dnSplitRunes = []rune{'/', ';', ',', '+'}

// Parse converts a string representation of a distinguished name into a pkix.Name.
//
// Supported attribute keys include C, O, OU, L, ST, STREET, POSTALCODE, SN, and CN.
// If the string contains no key=value pairs, it is treated as a bare CommonName.
func Parse(s string) (pkix.Name, error) {
	var n pkix.Name

	s = strings.TrimSpace(s)
	if s == "" {
		return n, fmt.Errorf("empty distinguished name")
	}

	// Treat as bare CommonName if no '=' present.
	if !strings.Contains(s, "=") {
		n.CommonName = s
		return n, nil
	}

	var sawKV bool
	parts := strings.FieldsFunc(s, func(r rune) bool { return slices.Contains(dnSplitRunes, r) })
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		sawKV = true

		k = strings.ToUpper(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}

		fn, ok := attributes[k]
		if !ok {
			return n, fmt.Errorf("unknown attribute %q", k)
		}
		fn(&n, v)
	}

	// Fallback to treating as CommonName if no valid key-value parsed.
	if !sawKV {
		n.CommonName = s
	}
	return n, nil
}

// Match verifies that name n satisfies all constraints present in pattern p.
//
// Fields in p act as constraints: if a field is non-empty in p,
// n must contain all of those values. CommonName is compared literally.
func Match(n, p pkix.Name) error {
	// Special case: CommonName must match if either side sets it.
	if n.CommonName != p.CommonName && (n.CommonName != "" || p.CommonName != "") {
		return fmt.Errorf(`common name %q does not match expected %q`, n.CommonName, p.CommonName)
	}

	// Verify multi-valued attributes.
	for _, f := range [...]struct {
		label string
		have  []string
		want  []string
	}{
		{"country", n.Country, p.Country},
		{"province", n.Province, p.Province},
		{"locality", n.Locality, p.Locality},
		{"postal code", n.PostalCode, p.PostalCode},
		{"street address", n.StreetAddress, p.StreetAddress},
		{"organization", n.Organization, p.Organization},
		{"organizational unit", n.OrganizationalUnit, p.OrganizationalUnit},
	} {
		if len(f.want) == 0 {
			continue
		}
		set := make(map[string]struct{}, len(f.have))
		for _, v := range f.have {
			set[v] = struct{}{}
		}
		for _, w := range f.want {
			if _, ok := set[w]; !ok {
				return fmt.Errorf(`%s %q does not match expected %q`, f.label, f.have, f.want)
			}
		}
	}
	return nil
}
