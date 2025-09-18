// Package rfc2253 implements parsing and matching of X.509 Distinguished
// Names according to RFC 2253.
//
// It provides utilities to convert string representations of distinguished
// names (as commonly used in certificates and LDAP) into Go’s
// crypto/x509/pkix.Name structure.
//
// Supported features:
//
//   - Splitting of relative distinguished names (RDNs) on unescaped `,` and `+`.
//   - Decoding of escaped values per RFC 2253 (§2.4), including special
//     characters, hex‐pairs (\xx), quoted values, trailing spaces, and
//     leading “#”.
//   - Decoding of BER-encoded values in `#hex` form for common string types
//     (UTF8String, PrintableString, IA5String, BMPString).
//   - Mapping of standard short attribute names (CN, C, O, OU, ST, L, etc.)
//     into pkix.Name fields.
//   - Mapping of many additional short names (e.g. UID, DC, emailAddress)
//     and dotted OIDs into pkix.Name.ExtraNames.
//   - Configurable behavior via Options
//   - Structural equality and subset checks with Equal and Match.
//
// The parser aims to be RFC-2253 compliant for practical certificate issuer checks,
// but does not attempt to support every historical quirk.
package rfc2253
