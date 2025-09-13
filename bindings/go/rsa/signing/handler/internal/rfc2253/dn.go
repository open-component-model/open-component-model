package rfc2253

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var wellKnownOID = map[string]asn1.ObjectIdentifier{
	"businesscategory":           {2, 5, 4, 15},
	"c":                          {2, 5, 4, 6},
	"cn":                         {2, 5, 4, 3},
	"dc":                         {0, 9, 2342, 19200300, 100, 1, 25},
	"description":                {2, 5, 4, 13},
	"destinationindicator":       {2, 5, 4, 27},
	"distinguishedname":          {2, 5, 4, 49},
	"dnqualifier":                {2, 5, 4, 46},
	"emailaddress":               {1, 2, 840, 113549, 1, 9, 1},
	"enhancedsearchguide":        {2, 5, 4, 47},
	"facsimiletelephonenumber":   {2, 5, 4, 23},
	"generationqualifier":        {2, 5, 4, 44},
	"givenname":                  {2, 5, 4, 42},
	"houseidentifier":            {2, 5, 4, 51},
	"initials":                   {2, 5, 4, 43},
	"internationalisdnnumber":    {2, 5, 4, 25},
	"l":                          {2, 5, 4, 7},
	"member":                     {2, 5, 4, 31},
	"name":                       {2, 5, 4, 41},
	"o":                          {2, 5, 4, 10},
	"ou":                         {2, 5, 4, 11},
	"owner":                      {2, 5, 4, 32},
	"physicaldeliveryofficename": {2, 5, 4, 19},
	"postaladdress":              {2, 5, 4, 16},
	"postalcode":                 {2, 5, 4, 17},
	"postofficebox":              {2, 5, 4, 18},
	"preferreddeliverymethod":    {2, 5, 4, 28},
	"registeredaddress":          {2, 5, 4, 26},
	"roleoccupant":               {2, 5, 4, 33},
	"searchguide":                {2, 5, 4, 14},
	"seealso":                    {2, 5, 4, 34},
	"serialnumber":               {2, 5, 4, 5},
	"sn":                         {2, 5, 4, 4},
	"st":                         {2, 5, 4, 8},
	"street":                     {2, 5, 4, 9},
	"telephonenumber":            {2, 5, 4, 20},
	"teletexterminalidentifier":  {2, 5, 4, 22},
	"telexnumber":                {2, 5, 4, 21},
	"title":                      {2, 5, 4, 12},
	"uid":                        {0, 9, 2342, 19200300, 100, 1, 1},
	"uniquemember":               {2, 5, 4, 50},
	"userpassword":               {2, 5, 4, 35},
	"x121address":                {2, 5, 4, 24},
}

// Options controls parsing behavior.
type Options struct {
	// Strict rejects unknown attributes and malformed AVAs.
	Strict bool
	// FallbackToCN puts the entire input into CN if nothing parsed.
	// This is a legacy behavior from OCMv1
	FallbackToCN bool
}

// Parse parses a distinguished name string (RFC 2253 subset) into pkix.Name
// using permissive defaults (non-strict, fallback to CN if empty).
func Parse(s string) (pkix.Name, error) {
	return ParseWithOptions(s, Options{Strict: false, FallbackToCN: true})
}

// ParseWithOptions parses with custom behavior.
func ParseWithOptions(s string, opt Options) (pkix.Name, error) {
	var n pkix.Name
	if strings.TrimSpace(s) == "" {
		return n, errors.New("empty distinguished name")
	}

	var parsed bool
	for _, rdn := range splitRFC2253(s, ',') {
		for _, ava := range splitRFC2253(rdn, '+') {
			k, v, ok := strings.Cut(ava, "=")
			if !ok {
				if opt.Strict {
					return n, fmt.Errorf("missing '=' in AVA %q", ava)
				}
				continue
			}
			k = strings.TrimSpace(k)
			val := parseRFC2253(v) // value whitespace can be significant

			switch strings.ToUpper(k) {
			case "C":
				n.Country = append(n.Country, val)
			case "O":
				n.Organization = append(n.Organization, val)
			case "OU":
				n.OrganizationalUnit = append(n.OrganizationalUnit, val)
			case "L":
				n.Locality = append(n.Locality, val)
			case "ST":
				n.Province = append(n.Province, val)
			case "STREET":
				n.StreetAddress = append(n.StreetAddress, val)
			case "POSTALCODE":
				n.PostalCode = append(n.PostalCode, val)
			case "SN", "SERIALNUMBER":
				n.SerialNumber = val
			case "CN":
				n.CommonName = val
			default:
				if oid, ok := shortOrOID(k); ok {
					n.ExtraNames = append(n.ExtraNames, pkix.AttributeTypeAndValue{Type: oid, Value: val})
				} else if opt.Strict {
					return n, fmt.Errorf("unknown attribute %q", k)
				}
			}
			parsed = true
		}
	}

	if !parsed && opt.FallbackToCN {
		n.CommonName = s
	}
	return n, nil
}

// Map short names and dotted OIDs.
func shortOrOID(s string) (asn1.ObjectIdentifier, bool) {
	key := strings.ToLower(strings.TrimSpace(s))
	if oid, ok := wellKnownOID[key]; ok {
		return oid, true
	}
	oid, err := stringToOID(s)
	if err == nil {
		return oid, true
	}
	return nil, false
}

// Equal reports structural equality of two distinguished names.
// It calls Match in both directions, meaning that every attribute in a
// is present in b, and every attribute in b is present in a.
// Equal ignores ordering differences allowed by RFC 2253.
func Equal(a, b pkix.Name) error {
	if err := Match(a, b); err != nil {
		return err
	}
	return Match(b, a)
}

// Match reports whether name n satisfies all constraints in pattern p.
//
// All scalar fields (CommonName, SerialNumber) must match exactly if set
// in p. All slice fields (Country, Province, Locality, PostalCode,
// StreetAddress, Organization, OrganizationalUnit) must contain at least
// the values listed in p. ExtraNames in p must all be present in n with
// matching OIDs and values.
//
// Match returns nil if n covers p, otherwise a descriptive error.
// It does not require n and p to be identical; that check is provided by Equal.
func Match(n, p pkix.Name) error {
	if p.CommonName != "" && n.CommonName != p.CommonName {
		return fmt.Errorf("common name %q does not match %q", n.CommonName, p.CommonName)
	}
	if p.SerialNumber != "" && n.SerialNumber != p.SerialNumber {
		return fmt.Errorf("serial number %q does not match %q", n.SerialNumber, p.SerialNumber)
	}

	type sf struct {
		label string
		have  []string
		want  []string
	}
	for _, f := range [...]sf{
		{"country", n.Country, p.Country},
		{"province", n.Province, p.Province},
		{"locality", n.Locality, p.Locality},
		{"postal code", n.PostalCode, p.PostalCode},
		{"street address", n.StreetAddress, p.StreetAddress},
		{"organization", n.Organization, p.Organization},
		{"organizational unit", n.OrganizationalUnit, p.OrganizationalUnit},
	} {
		if err := match(f.have, f.want); err != nil {
			return fmt.Errorf("%s %w", f.label, err)
		}
	}

outer:
	for _, w := range p.ExtraNames {
		for _, h := range n.ExtraNames {
			if w.Type.Equal(h.Type) && w.Value == h.Value {
				continue outer
			}
		}
		return fmt.Errorf("missing extra attribute %v=%v", w.Type, w.Value)
	}
	return nil
}

// stringToOID parses dotted decimal into asn1.ObjectIdentifier.
func stringToOID(s string) (asn1.ObjectIdentifier, error) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("not an OID: %q", s)
	}
	oid := make(asn1.ObjectIdentifier, len(parts))
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return nil, fmt.Errorf("invalid OID part %q", p)
		}
		oid[i] = v
	}
	return oid, nil
}

func match[T comparable](have, want []T) error {
	if len(want) == 0 {
		return nil
	}
	set := make(map[T]struct{}, len(have))
	for _, v := range have {
		set[v] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return fmt.Errorf("%v does not include required %v", have, want)
		}
	}
	return nil
}
