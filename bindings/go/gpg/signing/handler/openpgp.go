package handler

import (
	"bytes"
	"context"
	"crypto"
	"crypto/fips140"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

const armorBegin = "-----BEGIN PGP "

// go-crypto parses LibrePGP v5 keys and signatures (made by GnuPG and RNP) only if V5Disabled is false, which its
// v5 build tag would set at build time; setting it here keeps v5 support independent of how OCM is built. It is a
// process-wide go-crypto setting that only widens what parses.
func init() {
	packet.V5Disabled = false
}

// OpenPGP packet tags of key packets, whose first body byte is the key version.
const (
	tagSecretKey    = 5
	tagPublicKey    = 6
	tagSecretSubkey = 7
	tagPublicSubkey = 14
	tagSignature    = 2
)

// fipsBoundary applies the FIPS 140-3 mode rule (ADR 0030) to a step outside the Go Cryptographic Module:
// in strict mode (GODEBUG=fips140=only) it returns err, in FIPS mode it logs msg at debug level, otherwise it does nothing.
func fipsBoundary(ctx context.Context, err error, msg string, attrs ...any) error {
	if fips140.Enforced() {
		return err
	}
	if fips140.Enabled() {
		slog.DebugContext(ctx, msg+" runs outside the FIPS 140-3 boundary (GODEBUG=fips140=only rejects it)", attrs...)
	}
	return nil
}

// readKeyRing parses binary or ASCII-armored key material, which may hold several concatenated armored blocks.
// All key packets are screened before parsing (see screenKeyPackets). Unlike openpgp.ReadKeyRing alone,
// it fails on key material it cannot parse instead of skipping it.
func readKeyRing(ctx context.Context, material []byte) (openpgp.EntityList, error) {
	bodies, err := keyBodies(material)
	if err != nil {
		return nil, err
	}
	for i, body := range bodies {
		if bodies[i], err = screenKeyPackets(ctx, body); err != nil {
			return nil, err
		}
	}
	var entities openpgp.EntityList
	for _, body := range bodies {
		el, err := openpgp.ReadKeyRing(bytes.NewReader(body))
		if err != nil {
			return nil, errParseKeyMaterial(err)
		}
		if len(el) == 0 {
			return nil, errors.New("no OpenPGP key found in key material")
		}
		entities = append(entities, el...)
	}
	return entities, nil
}

// errParseKeyMaterial wraps an error of go-crypto parsing key material. GnuPG exports a secret key that lives on a
// smartcard as a stub with the GNU S2K extension "divert-to-card", which go-crypto reports as unsupported.
func errParseKeyMaterial(err error) error {
	var unsupported pgperrors.UnsupportedError
	if errors.As(err, &unsupported) && string(unsupported) == "GNU S2K extension" {
		return fmt.Errorf("parse OpenPGP key material: the secret key is stored on a hardware token, which is not supported: %w", err)
	}
	return fmt.Errorf("parse OpenPGP key material: %w", err)
}

// keyBodies returns the binary packet sequences of the key material: the material itself if it is binary,
// otherwise the body of every armored block. armor.Decode reads only one block and consumes input past it,
// so each block is decoded from its own slice.
func keyBodies(material []byte) ([][]byte, error) {
	start := bytes.Index(material, []byte(armorBegin))
	if start < 0 {
		return [][]byte{material}, nil
	}
	var bodies [][]byte
	rest := material[start:]
	for len(rest) > 0 {
		chunk := rest
		if next := bytes.Index(rest[len(armorBegin):], []byte(armorBegin)); next >= 0 {
			chunk, rest = rest[:len(armorBegin)+next], rest[len(armorBegin)+next:]
		} else {
			rest = nil
		}
		block, err := armor.Decode(bytes.NewReader(chunk))
		if err != nil {
			return nil, fmt.Errorf("decode armored key material: %w", err)
		}
		if block.Type != openpgp.PublicKeyType && block.Type != openpgp.PrivateKeyType {
			return nil, fmt.Errorf("unexpected armored block %q in key material", block.Type)
		}
		body, err := io.ReadAll(block.Body)
		if err != nil {
			return nil, fmt.Errorf("decode armored key material: %w", err)
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

// User ID and user attribute packets end the signatures of the preceding key packet, like a key packet.
const (
	tagUserID        = 13
	tagUserAttribute = 17
)

// screenKeyPackets prepares a packet sequence for openpgp.ReadKeyRing and applies the FIPS boundary before it parses:
//   - Encryption-only subkeys are dropped together with their binding signatures. Signing and verification never use
//     them, and go-crypto fails on the whole key for subkey algorithms it does not know, such as GnuPG's Kyber.
//   - v4 and older keys are identified by a SHA-1 fingerprint, which panics in strict mode, so the version is read
//     from the raw packet and the packet is parsed only if it passes. v5 (LibrePGP) and v6 (RFC 9580) keys use SHA-256.
//   - Parsing verifies the self-signatures and binding signatures with the primary key and the back-signatures with
//     signing subkeys, so every signing-capable key must use an approved algorithm, not only the key that signs.
func screenKeyPackets(ctx context.Context, body []byte) ([]byte, error) {
	var (
		out           bytes.Buffer
		dropping      bool
		versionLogged bool
	)
	r := packet.NewOpaqueReader(bytes.NewReader(body))
	for {
		op, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, errParseKeyMaterial(err)
		}
		switch op.Tag {
		case tagSecretKey, tagPublicKey, tagSecretSubkey, tagPublicSubkey:
			// v4, v5 and v6 key packets start with version (1 byte), creation time (4) and public key algorithm (1).
			if len(op.Contents) < 6 {
				return nil, errParseKeyMaterial(errors.New("truncated key packet"))
			}
			canSign := packet.PublicKeyAlgorithm(op.Contents[5]).CanSign()
			dropping = !canSign && (op.Tag == tagSecretSubkey || op.Tag == tagPublicSubkey)
			if dropping {
				continue
			}
			if v := op.Contents[0]; v != 5 && v != 6 && !versionLogged {
				if err := fipsBoundary(ctx, fmt.Errorf("%w (found a version %d key)", ErrV4KeyInFIPSMode, v),
					"OpenPGP key with a SHA-1 fingerprint", "keyVersion", v); err != nil {
					return nil, err
				}
				versionLogged = true
			}
			if canSign {
				pk, err := parseKeyPacket(op)
				if err != nil {
					return nil, errParseKeyMaterial(err)
				}
				if err := checkKeyAlgorithm(ctx, pk); err != nil {
					return nil, err
				}
			}
		case tagUserID, tagUserAttribute:
			dropping = false
		default:
			if dropping {
				continue
			}
		}
		if err := op.Serialize(&out); err != nil {
			return nil, errParseKeyMaterial(err)
		}
	}
}

// parseKeyPacket parses a single public or secret (sub)key packet without verifying anything.
func parseKeyPacket(op *packet.OpaquePacket) (*packet.PublicKey, error) {
	var buf bytes.Buffer
	if err := op.Serialize(&buf); err != nil {
		return nil, err
	}
	p, err := packet.Read(&buf)
	if err != nil {
		return nil, err
	}
	switch k := p.(type) {
	case *packet.PublicKey:
		return k, nil
	case *packet.PrivateKey:
		return &k.PublicKey, nil
	default:
		return nil, fmt.Errorf("unexpected packet %T for key packet tag %d", p, op.Tag)
	}
}

// matchesFingerprint compares a key against a full fingerprint or a 16-hex-character long key ID.
func matchesFingerprint(pk *packet.PublicKey, want string) bool {
	return strings.EqualFold(hex.EncodeToString(pk.Fingerprint), want) ||
		(len(want) == 16 && strings.EqualFold(fmt.Sprintf("%016X", pk.KeyId), want))
}

// selectSigningKey returns the key that signs: the signing key of the first entity with a secret key,
// or, if want is set, of the first such entity whose primary key or a subkey matches want.
func selectSigningKey(entities openpgp.EntityList, want string, now time.Time) (openpgp.Key, error) {
	e, keyID, err := signingEntity(entities, want)
	if err != nil {
		return openpgp.Key{}, err
	}
	k, ok := e.SigningKeyById(now, keyID)
	if !ok {
		return openpgp.Key{}, fmt.Errorf("key %X has no valid signing key (expired, revoked, or without signing capability)", e.PrimaryKey.Fingerprint)
	}
	if k.PrivateKey == nil {
		return openpgp.Key{}, fmt.Errorf("private key material lacks the secret part of signing key %X", k.PublicKey.Fingerprint)
	}
	if k.PrivateKey.Dummy() {
		return openpgp.Key{}, fmt.Errorf("signing key %X is a stub without secret key material, as gpg --export-secret-subkeys exports an offline primary key; provide a signing subkey with its secret part", k.PublicKey.Fingerprint)
	}
	return k, nil
}

// signingEntity returns the first entity with a secret key whose primary key or a subkey matches want (any, if want
// is empty), and the key ID of the matching subkey, or 0 to let the entity choose its signing key.
func signingEntity(entities openpgp.EntityList, want string) (*openpgp.Entity, uint64, error) {
	hasSecret := false
	for _, e := range entities {
		if e.PrivateKey == nil {
			continue
		}
		hasSecret = true
		if want == "" || matchesFingerprint(e.PrimaryKey, want) {
			return e, 0, nil
		}
		for _, s := range e.Subkeys {
			if matchesFingerprint(s.PublicKey, want) {
				return e, s.PublicKey.KeyId, nil
			}
		}
	}
	if !hasSecret {
		return nil, 0, ErrNoSecretKey
	}
	return nil, 0, fmt.Errorf("no secret key matching key fingerprint %q found in private key material", want)
}

func algorithmName(pk *packet.PublicKey) string {
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSASignOnly:
		return "RSA"
	case packet.PubKeyAlgoECDSA:
		curve, _ := pk.Curve()
		return "ECDSA " + string(curve)
	case packet.PubKeyAlgoEdDSA:
		return "EdDSA"
	case packet.PubKeyAlgoEd25519:
		return "Ed25519"
	case packet.PubKeyAlgoEd448:
		return "Ed448"
	case packet.PubKeyAlgoDSA:
		return "DSA"
	default:
		return fmt.Sprintf("public key algorithm %d", pk.PubKeyAlgo)
	}
}

// checkKeyAlgorithm applies the FIPS boundary to keys whose algorithm the Go Cryptographic Module does not implement.
// RSA sizes below 2048 bits are left to the module, which rejects them in strict mode.
func checkKeyAlgorithm(ctx context.Context, pk *packet.PublicKey) error {
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSASignOnly:
		return nil
	case packet.PubKeyAlgoECDSA:
		switch curve, _ := pk.Curve(); curve {
		case packet.CurveNistP256, packet.CurveNistP384, packet.CurveNistP521:
			return nil
		}
	}
	name := algorithmName(pk)
	return fipsBoundary(ctx, fmt.Errorf("%w: key %X uses %s", ErrAlgorithmInFIPSMode, pk.Fingerprint, name),
		"GPG key algorithm "+name, "key", fmt.Sprintf("%X", pk.Fingerprint))
}

// parseSignature parses an ASCII-armored detached signature that must consist of exactly one signature packet,
// because openpgp.VerifyDetachedSignature checks only the first signature with a known issuer and ignores the rest.
// It returns the signature and the binary packet.
func parseSignature(value string) (*packet.Signature, []byte, error) {
	switch n := strings.Count(value, armorBegin); n {
	case 0:
		return nil, nil, errors.New("GPG signature is not an ASCII-armored OpenPGP signature")
	case 1:
	default:
		return nil, nil, fmt.Errorf("GPG signature contains %d armored blocks, but must contain exactly one signature", n)
	}
	block, err := armor.Decode(strings.NewReader(value))
	if err != nil {
		return nil, nil, fmt.Errorf("decode GPG signature: %w", err)
	}
	if block.Type != openpgp.SignatureType {
		return nil, nil, fmt.Errorf("GPG signature is an armored %q block, expected %q", block.Type, openpgp.SignatureType)
	}
	body, err := io.ReadAll(block.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("decode GPG signature: %w", err)
	}

	var packets, signatures int
	r := packet.NewOpaqueReader(bytes.NewReader(body))
	for {
		p, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parse GPG signature: %w", err)
		}
		packets++
		if p.Tag == tagSignature {
			signatures++
		}
	}
	if packets != 1 || signatures != 1 {
		return nil, nil, fmt.Errorf("GPG signature must contain exactly one signature packet and nothing else, found %d packets of which %d are signatures", packets, signatures)
	}

	p, err := packet.Read(bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("parse GPG signature: %w", err)
	}
	sig, ok := p.(*packet.Signature)
	if !ok {
		return nil, nil, fmt.Errorf("parse GPG signature: unexpected packet %T", p)
	}
	switch sig.Hash {
	case crypto.SHA256, crypto.SHA384, crypto.SHA512:
	default:
		return nil, nil, fmt.Errorf("GPG signature uses unsupported hash algorithm %v; expected SHA-256, SHA-384 or SHA-512", sig.Hash)
	}
	if sig.IssuerKeyId == nil {
		return nil, nil, errors.New("GPG signature has no issuer key ID")
	}
	return sig, body, nil
}

// hashForAlgorithm maps the configured hash algorithm to the hash of the OpenPGP signature; 0 if none is configured.
// Unknown or misspelled values are an error so that callers don't silently get another hash.
func hashForAlgorithm(alg v1alpha1.HashAlgorithm) (crypto.Hash, error) {
	switch alg {
	case "":
		return 0, nil
	case v1alpha1.HashAlgorithmSHA256:
		return crypto.SHA256, nil
	case v1alpha1.HashAlgorithmSHA384:
		return crypto.SHA384, nil
	case v1alpha1.HashAlgorithmSHA512:
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("unsupported GPG hash algorithm %q", alg)
	}
}

// signatureHash returns the hash to sign with pk: the configured hash, or, if none is configured (0), SHA-256 or the
// longer hash pk needs. ECDSA and EdDSA need a hash at least as long as the curve's group order (capped at SHA-512),
// Ed448 needs SHA-512. go-crypto would silently replace a shorter hash and GnuPG refuses to sign with one, so a
// configured hash that is too short is an error. The minimums mirror go-crypto's acceptableHashesToWrite
// (openpgp/write.go); TestGPGHandler_SignatureHash fails if they drift apart.
func signatureHash(configured crypto.Hash, pk *packet.PublicKey) (crypto.Hash, error) {
	minimum := crypto.SHA256
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoEd448:
		minimum = crypto.SHA512
	case packet.PubKeyAlgoECDSA, packet.PubKeyAlgoEdDSA:
		switch curve, _ := pk.Curve(); curve {
		case packet.CurveNistP384, packet.CurveBrainpoolP384:
			minimum = crypto.SHA384
		case packet.CurveNistP521, packet.CurveBrainpoolP512, packet.Curve448:
			minimum = crypto.SHA512
		}
	}
	if configured == 0 {
		return minimum, nil
	}
	if hashBits(configured) < hashBits(minimum) {
		return 0, fmt.Errorf("%w: signing key %X (%s) needs %s or longer; set hashAlgorithm to it or remove hashAlgorithm",
			ErrHashTooShortForKey, pk.Fingerprint, algorithmName(pk), minimum)
	}
	return configured, nil
}

func hashBits(h crypto.Hash) int {
	return h.Size() * 8
}
