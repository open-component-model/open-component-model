package tsa

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const nonceBytes = 16

// Request is an RFC 3161 TimeStampReq.
//
//	TimeStampReq ::= SEQUENCE {
//	  version          INTEGER  { v1(1) },
//	  messageImprint   MessageImprint,
//	  reqPolicy        TSAPolicyId           OPTIONAL,
//	  nonce            INTEGER               OPTIONAL,
//	  certReq          BOOLEAN               DEFAULT FALSE,
//	  extensions       [0] IMPLICIT Extensions OPTIONAL }
type Request struct {
	Version        int
	MessageImprint MessageImprint
	ReqPolicy      asn1.ObjectIdentifier `asn1:"optional"`
	Nonce          *big.Int              `asn1:"optional"`
	CertReq        bool                  `asn1:"optional,default:false"`
}

// Response is an RFC 3161 TimeStampResp.
//
//	TimeStampResp ::= SEQUENCE {
//	  status           PKIStatusInfo,
//	  timeStampToken   TimeStampToken OPTIONAL }
//
//	TimeStampToken ::= ContentInfo
type Response struct {
	Status         PKIStatusInfo
	TimeStampToken asn1.RawValue `asn1:"optional"`
}

// Info is an RFC 3161 TSTInfo, the core payload inside a timestamp token.
//
//	TSTInfo ::= SEQUENCE {
//	  version          INTEGER  { v1(1) },
//	  policy           TSAPolicyId,
//	  messageImprint   MessageImprint,
//	  serialNumber     INTEGER,
//	  genTime          GeneralizedTime,
//	  accuracy         Accuracy               OPTIONAL,
//	  ordering         BOOLEAN                 DEFAULT FALSE,
//	  nonce            INTEGER                 OPTIONAL,
//	  tsa              [0] GeneralName         OPTIONAL,
//	  extensions       [1] IMPLICIT Extensions OPTIONAL }
type Info struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint MessageImprint
	SerialNumber   *big.Int
	GenTime        time.Time        `asn1:"generalized"`
	Accuracy       Accuracy         `asn1:"optional"`
	Ordering       bool             `asn1:"optional,default:false"`
	Nonce          *big.Int         `asn1:"optional"`
	TSA            asn1.RawValue    `asn1:"tag:0,optional"`
	Extensions     []pkix.Extension `asn1:"tag:1,optional"`
}

// MessageImprint carries a hash algorithm identifier and the digest value
// of the datum to be timestamped.
//
//	MessageImprint ::= SEQUENCE {
//	  hashAlgorithm   AlgorithmIdentifier,
//	  hashedMessage   OCTET STRING }
type MessageImprint struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	HashedMessage []byte
}

// NewMessageImprint creates a MessageImprint from a crypto.Hash and a
// pre-computed digest. It returns an error if the hash is unsupported or
// the digest length does not match.
func NewMessageImprint(hash crypto.Hash, digest []byte) (MessageImprint, error) {
	oid, ok := hashToDigestAlgorithm[hash]
	if !ok {
		return MessageImprint{}, fmt.Errorf("tsa: unsupported hash algorithm %v", hash)
	}
	if !hash.Available() {
		return MessageImprint{}, fmt.Errorf("tsa: hash algorithm %v not available", hash)
	}
	if len(digest) != hash.Size() {
		return MessageImprint{}, fmt.Errorf("tsa: digest length %d does not match %v size %d", len(digest), hash, hash.Size())
	}
	return MessageImprint{
		HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oid},
		HashedMessage: digest,
	}, nil
}

// Hash returns the crypto.Hash for this MessageImprint's algorithm.
func (mi MessageImprint) Hash() (crypto.Hash, error) {
	h, ok := digestAlgorithmToHash[mi.HashAlgorithm.Algorithm.String()]
	if !ok || !h.Available() {
		return 0, fmt.Errorf("tsa: unsupported digest algorithm OID %s", mi.HashAlgorithm.Algorithm)
	}
	return h, nil
}

// Equal reports whether two MessageImprints are identical.
func (mi MessageImprint) Equal(other MessageImprint) bool {
	if !mi.HashAlgorithm.Algorithm.Equal(other.HashAlgorithm.Algorithm) {
		return false
	}
	return bytes.Equal(mi.HashedMessage, other.HashedMessage)
}

// Accuracy represents the optional accuracy of the TSA's clock.
//
//	Accuracy ::= SEQUENCE {
//	  seconds   INTEGER           OPTIONAL,
//	  millis    [0] INTEGER (1..999) OPTIONAL,
//	  micros    [1] INTEGER (1..999) OPTIONAL }
type Accuracy struct {
	Seconds int `asn1:"optional"`
	Millis  int `asn1:"tag:0,optional"`
	Micros  int `asn1:"tag:1,optional"`
}

// Duration converts the Accuracy to a time.Duration.
func (a Accuracy) Duration() time.Duration {
	return time.Duration(a.Seconds)*time.Second +
		time.Duration(a.Millis)*time.Millisecond +
		time.Duration(a.Micros)*time.Microsecond
}

// PKIStatusInfo carries the TSA's response status.
//
//	PKIStatusInfo ::= SEQUENCE {
//	  status        PKIStatus,
//	  statusString  PKIFreeText     OPTIONAL,
//	  failInfo      PKIFailureInfo  OPTIONAL }
type PKIStatusInfo struct {
	Status       int
	StatusString []asn1.RawValue `asn1:"optional"`
	FailInfo     asn1.BitString  `asn1:"optional"`
}

// PKIStatus constants.
const (
	StatusGranted                = 0
	StatusGrantedWithMods       = 1
	StatusRejection             = 2
	StatusWaiting               = 3
	StatusRevocationWarning     = 4
	StatusRevocationNotification = 5
)

// Err returns nil if the status indicates success (granted or grantedWithMods),
// or an error describing the failure.
func (si PKIStatusInfo) Err() error {
	if si.Status == StatusGranted || si.Status == StatusGrantedWithMods {
		return nil
	}

	fiStr := ""
	if si.FailInfo.BitLength > 0 {
		bits := make([]byte, si.FailInfo.BitLength)
		for i := range bits {
			if si.FailInfo.At(i) == 1 {
				bits[i] = '1'
			} else {
				bits[i] = '0'
			}
		}
		fiStr = fmt.Sprintf(" FailInfo(0b%s)", string(bits))
	}

	statusStr := ""
	if len(si.StatusString) > 0 {
		var parts []string
		for _, raw := range si.StatusString {
			var s string
			if _, err := asn1.Unmarshal(raw.FullBytes, &s); err == nil {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			statusStr = fmt.Sprintf(" StatusString(%s)", strings.Join(parts, ", "))
		}
	}

	return fmt.Errorf("tsa: bad response: Status(%d)%s%s", si.Status, statusStr, fiStr)
}

// GenerateNonce creates a cryptographically random nonce suitable for a
// timestamp request.
func GenerateNonce() (*big.Int, error) {
	buf := make([]byte, nonceBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("tsa: failed to generate nonce: %w", err)
	}
	return new(big.Int).SetBytes(buf), nil
}
