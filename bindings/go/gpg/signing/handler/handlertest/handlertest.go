// Package handlertest generates OpenPGP keys for tests of the GPG signing handler and its callers.
//
// Under GODEBUG=fips140=only, generate keys inside fips140.WithoutEnforcement: key generation is
// fixture setup, and v4 keys need SHA-1 for their fingerprint.
package handlertest

import (
	"bytes"
	"crypto"
	"fmt"
	"io"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// KeyConfig selects the key GenerateKey creates. The zero value is an unprotected v4 RSA-3072 key whose primary key signs.
type KeyConfig struct {
	V6            bool                      // RFC 9580 v6 key instead of v4
	Algorithm     packet.PublicKeyAlgorithm // 0 means packet.PubKeyAlgoRSA
	Curve         packet.Curve              // for ECDSA / EdDSA, e.g. packet.CurveNistP256, packet.Curve25519
	Passphrase    string                    // non-empty: secret keys are passphrase-protected in Private
	SigningSubkey bool                      // add a signing subkey (go-crypto then signs with it)
	// SubkeyAlgorithm and SubkeyCurve select the algorithm of the signing subkey; 0 and "" mean the primary key's.
	SubkeyAlgorithm packet.PublicKeyAlgorithm
	SubkeyCurve     packet.Curve
	Created         time.Time     // creation time of the key and its self-signatures; zero means now
	Lifetime        time.Duration // key expiry after Created; 0 means the key never expires
}

// Key is an OpenPGP key pair.
type Key struct {
	Private     string          // ASCII-armored secret key ("PGP PRIVATE KEY BLOCK")
	Public      string          // ASCII-armored public key ("PGP PUBLIC KEY BLOCK")
	Fingerprint string          // upper-case hex fingerprint of the primary key
	Entity      *openpgp.Entity // unprotected entity, for test setups (revocations, custom signatures)
}

// GenerateKey creates a key pair as selected by cfg.
func GenerateKey(cfg KeyConfig) (*Key, error) {
	pcfg := &packet.Config{
		V6Keys:          cfg.V6,
		Algorithm:       cfg.Algorithm,
		RSABits:         3072,
		Curve:           cfg.Curve,
		DefaultHash:     crypto.SHA256,
		KeyLifetimeSecs: uint32(cfg.Lifetime / time.Second), //nolint:gosec // test key lifetimes are far below 136 years
	}
	if pcfg.Algorithm == 0 {
		pcfg.Algorithm = packet.PubKeyAlgoRSA
	}
	if !cfg.Created.IsZero() {
		pcfg.Time = func() time.Time { return cfg.Created }
	}
	e, err := openpgp.NewEntity("OCM Test", "", "ocm-test@example.com", pcfg)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if cfg.SigningSubkey {
		scfg := *pcfg
		if cfg.SubkeyAlgorithm != 0 {
			scfg.Algorithm, scfg.Curve = cfg.SubkeyAlgorithm, cfg.SubkeyCurve
		}
		if err := e.AddSigningSubkey(&scfg); err != nil {
			return nil, fmt.Errorf("add signing subkey: %w", err)
		}
	}

	public, err := ArmoredPublicKey(e)
	if err != nil {
		return nil, err
	}
	if cfg.Passphrase != "" {
		if err := e.EncryptPrivateKeys([]byte(cfg.Passphrase), pcfg); err != nil {
			return nil, fmt.Errorf("protect private key: %w", err)
		}
	}
	private, err := armorBlock(openpgp.PrivateKeyType, func(w io.Writer) error {
		return e.SerializePrivateWithoutSigning(w, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("serialize private key: %w", err)
	}
	if cfg.Passphrase != "" {
		if err := e.DecryptPrivateKeys([]byte(cfg.Passphrase)); err != nil {
			return nil, fmt.Errorf("unlock private key: %w", err)
		}
	}

	return &Key{
		Private:     private,
		Public:      public,
		Fingerprint: fmt.Sprintf("%X", e.PrimaryKey.Fingerprint),
		Entity:      e,
	}, nil
}

// ArmoredPublicKey returns the ASCII-armored public key of e, including revocations added after GenerateKey.
func ArmoredPublicKey(e *openpgp.Entity) (string, error) {
	s, err := armorBlock(openpgp.PublicKeyType, e.Serialize)
	if err != nil {
		return "", fmt.Errorf("serialize public key: %w", err)
	}
	return s, nil
}

func armorBlock(blockType string, serialize func(io.Writer) error) (string, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, blockType, nil)
	if err != nil {
		return "", err
	}
	if err := serialize(w); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
