// Package tsa provides an RFC 3161 Timestamping Authority client for
// requesting and verifying timestamps on OCM component version signatures.
package tsa

import (
	"crypto"
	"encoding/asn1"
)

// Well-known OIDs used in RFC 3161 timestamp operations.
var (
	oidDigestAlgorithmSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidDigestAlgorithmSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidDigestAlgorithmSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
)

// digestAlgorithmToHash maps digest algorithm OID strings to crypto.Hash values.
var digestAlgorithmToHash = map[string]crypto.Hash{
	oidDigestAlgorithmSHA256.String(): crypto.SHA256,
	oidDigestAlgorithmSHA384.String(): crypto.SHA384,
	oidDigestAlgorithmSHA512.String(): crypto.SHA512,
}

// hashToDigestAlgorithm maps crypto.Hash values to digest algorithm OIDs.
var hashToDigestAlgorithm = map[crypto.Hash]asn1.ObjectIdentifier{
	crypto.SHA256: oidDigestAlgorithmSHA256,
	crypto.SHA384: oidDigestAlgorithmSHA384,
	crypto.SHA512: oidDigestAlgorithmSHA512,
}
