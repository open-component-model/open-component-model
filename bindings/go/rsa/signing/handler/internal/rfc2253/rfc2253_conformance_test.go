package rfc2253_test

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/rfc2253"
)

func TestRFC2253_Conformance(t *testing.T) {
	t.Parallel()

	type tc struct {
		name       string
		input      string
		wantCN     string
		wantString string
		// ExtraNames expectations
		wantExtra int
		extraType string
		extraVal  string
		// Match expectations
		matchRef *pkix.Name
		matchOK  bool
	}

	uidOID := asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 1} // UID
	emailOID := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}    // emailAddress

	cases := []tc{
		{name: "plain", input: "CN=John Doe", wantCN: "John Doe", wantString: "CN=John Doe"},
		{name: "quoted", input: "CN=\"John Doe\"", wantCN: "John Doe", wantString: "CN=John Doe"},
		{name: "escaped comma", input: "CN=John\\,Doe", wantCN: "John,Doe", wantString: "CN=John\\,Doe"},
		{name: "escaped plus", input: "CN=Sales\\+Marketing", wantCN: "Sales+Marketing", wantString: "CN=Sales\\+Marketing"},
		{name: "escaped equal", input: "CN=ACME\\=Inc", wantCN: "ACME=Inc", wantString: "CN=ACME=Inc"},
		{name: "trailing space", input: "CN=Space\\  ", wantCN: "Space ", wantString: "CN=Space\\ "},
		{name: "hexpair", input: "CN=John\\20Doe", wantCN: "John Doe", wantString: "CN=John Doe"},
		{name: "LF", input: "CN=\\0AJohn", wantCN: "\nJohn", wantString: "CN=\nJohn"},
		{name: "bang", input: "CN=\\21Hello", wantCN: "!Hello", wantString: "CN=!Hello"},
		{name: "multiple hex", input: "CN=\\41\\42\\43", wantCN: "ABC", wantString: "CN=ABC"},
		{name: "mixed escapes", input: "CN=ACME\\2C Inc\\20\\28Europe\\29", wantCN: "ACME, Inc (Europe)", wantString: "CN=ACME\\, Inc (Europe)"},
		{name: "trailing backslash", input: "CN=Foo\\", wantCN: "Foo\\", wantString: "CN=Foo\\\\"},
		{name: "PrintableString", input: "CN=#1303414243", wantCN: "ABC", wantString: "CN=ABC"},
		{name: "UTF8String", input: "CN=#0C02C3BC", wantCN: "ü", wantString: "CN=ü"},
		{name: "IA5String", input: "CN=#16096D61696C4074657374", wantCN: "mail@test", wantString: "CN=mail@test"},
		{name: "BMPString", input: "CN=#1E0400480069", wantCN: "Hi", wantString: "CN=Hi"},
		{name: "Invalid hex", input: "CN=#NOTHEX", wantCN: "#NOTHEX", wantString: "CN=\\#NOTHEX"},

		// ExtraNames from known short name
		{name: "UID ExtraName", input: "UID=alice", wantExtra: 1, extraType: uidOID.String(), extraVal: "alice"},
		// ExtraNames from dotted OID
		{name: "OID ExtraName", input: "1.2.840.113549.1.9.1=bob@example.com", wantExtra: 1, extraType: emailOID.String(), extraVal: "bob@example.com"},
		// Multiple ExtraNames
		{name: "Multiple ExtraNames", input: "UID=alice+DC=example", wantExtra: 2},

		// Match ExtraNames positive
		{name: "Match ExtraNames ok", input: "UID=alice", matchRef: &pkix.Name{ExtraNames: []pkix.AttributeTypeAndValue{{Type: uidOID, Value: "alice"}}}, matchOK: true},
		// Match ExtraNames missing
		{name: "Match ExtraNames missing", input: "CN=test", matchRef: &pkix.Name{ExtraNames: []pkix.AttributeTypeAndValue{{Type: uidOID, Value: "alice"}}}, matchOK: false},
		// Match ExtraNames wrong value
		{name: "Match ExtraNames wrong", input: "UID=bob", matchRef: &pkix.Name{ExtraNames: []pkix.AttributeTypeAndValue{{Type: uidOID, Value: "alice"}}}, matchOK: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := rfc2253.Parse(c.input)
			require.NoError(t, err)

			// CN and String checks if expected
			if c.wantCN != "" && got.CommonName != c.wantCN {
				require.Equal(t, got.CommonName, c.wantCN, "CommonName mismatch")
			}
			if c.wantString != "" {
				require.Equal(t, got.String(), c.wantString, "String mismatch")
			}

			// ExtraNames checks
			if c.wantExtra > 0 {
				require.Len(t, got.ExtraNames, c.wantExtra, "ExtraNames length mismatch")
				if c.extraType != "" {
					require.Equal(t, got.ExtraNames[0].Type.String(), c.extraType, "ExtraNames OID mismatch")
				}
				if c.extraVal != "" && got.ExtraNames[0].Value != c.extraVal {
					require.Equal(t, got.ExtraNames[0].Value, c.extraVal, "ExtraNames Value mismatch")
				}
			}

			// Match checks
			if c.matchRef != nil {
				if err := rfc2253.Match(got, *c.matchRef); c.matchOK {
					require.NoError(t, err, "Match failed")
				} else {
					require.Error(t, err, "Match succeeded")
				}
			}
		})
	}
}
