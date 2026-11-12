package replay_test

// Offline verification tests: the real handler + pinned cosign verifying the
// recorded bundle against the committed trusted root. No scaffolding cluster and
// no SIGSTORE_* env are needed. These are the verify-path tests moved out of
// sigstore/integration; the live sign path stays there.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
	trustedrootv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/trustedroot/v1alpha1"
)

type bundleJSON struct {
	VerificationMaterial struct {
		Certificate *struct {
			RawBytes string `json:"rawBytes"`
		} `json:"certificate"`
		TlogEntries []json.RawMessage `json:"tlogEntries"`
	} `json:"verificationMaterial"`
	MessageSignature *struct {
		Signature string `json:"signature"`
	} `json:"messageSignature"`
}

func decodeBundle(t *testing.T, value string) *bundleJSON {
	t.Helper()
	r := require.New(t)
	raw, err := base64.StdEncoding.DecodeString(value)
	r.NoError(err)
	var b bundleJSON
	r.NoError(json.Unmarshal(raw, &b))
	return &b
}

func Test_Replay_IdentityVerification(t *testing.T) {
	h := newHandler(t)
	signed := recordedSignature(t, "identity-test")
	tr := trustedRoot(t)

	t.Run("matching issuer succeeds", func(t *testing.T) {
		r := require.New(t)
		r.NoError(h.Verify(t.Context(), signed, verifyConfig(t), tr))
	})

	t.Run("wrong issuer fails", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(t, func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = "https://wrong-issuer.example.com"
		})
		err := h.Verify(t.Context(), signed, cfg, tr)
		r.Error(err)
		r.ErrorContains(err, "issuer")
	})

	t.Run("issuer regex succeeds", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(t, func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = ""
			c.CertificateOIDCIssuerRegexp = ".*"
		})
		r.NoError(h.Verify(t.Context(), signed, cfg, tr))
	})

	t.Run("matching identity succeeds", func(t *testing.T) {
		r := require.New(t)
		m := loadMeta(t)
		cfg := verifyConfig(t, func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = ""
			c.CertificateOIDCIssuerRegexp = ".*"
			c.CertificateIdentityRegexp = ""
			c.CertificateIdentity = m.OIDCIdentity
		})
		r.NoError(h.Verify(t.Context(), signed, cfg, tr))
	})

	t.Run("wrong identity fails", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(t, func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = ""
			c.CertificateOIDCIssuerRegexp = ".*"
			c.CertificateIdentityRegexp = ""
			c.CertificateIdentity = "wrong@example.com"
		})
		r.Error(h.Verify(t.Context(), signed, cfg, tr))
	})
}

func Test_Replay_TamperedBundle(t *testing.T) {
	h := newHandler(t)
	signed := recordedSignature(t, "tamper-baseline")
	tr := trustedRoot(t)
	m := loadMeta(t)
	verifyCfg := verifyConfig(t, func(c *v1alpha1.VerifyConfig) {
		c.CertificateIdentityRegexp = ""
		c.CertificateIdentity = m.OIDCIdentity
	})

	require.New(t).NoError(h.Verify(t.Context(), signed, verifyCfg, tr), "baseline verification must succeed")

	mutateBundle := func(t *testing.T, f func(m map[string]any)) string {
		t.Helper()
		r := require.New(t)
		raw, err := base64.StdEncoding.DecodeString(signed.Signature.Value)
		r.NoError(err)
		var mm map[string]any
		r.NoError(json.Unmarshal(raw, &mm))
		f(mm)
		modified, err := json.Marshal(mm)
		r.NoError(err)
		return base64.StdEncoding.EncodeToString(modified)
	}

	tamperedSignature := func(name, value string) descruntime.Signature {
		return descruntime.Signature{
			Name:   name,
			Digest: signed.Digest,
			Signature: descruntime.SignatureInfo{
				Algorithm: signed.Signature.Algorithm,
				MediaType: signed.Signature.MediaType,
				Value:     value,
			},
		}
	}

	t.Run("mutated signature bytes rejected", func(t *testing.T) {
		r := require.New(t)
		b := decodeBundle(t, signed.Signature.Value)
		r.NotNil(b.MessageSignature)
		sigBytes, err := base64.StdEncoding.DecodeString(b.MessageSignature.Signature)
		r.NoError(err)
		r.NotEmpty(sigBytes)
		sigBytes[len(sigBytes)-1] ^= 0xFF
		tampered := mutateBundle(t, func(mm map[string]any) {
			ms := mm["messageSignature"].(map[string]any)
			ms["signature"] = base64.StdEncoding.EncodeToString(sigBytes)
		})
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-sig-bytes", tampered), verifyCfg, tr))
	})

	t.Run("stripped certificate rejected", func(t *testing.T) {
		r := require.New(t)
		tampered := mutateBundle(t, func(mm map[string]any) {
			vm := mm["verificationMaterial"].(map[string]any)
			delete(vm, "certificate")
		})
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-strip-cert", tampered), verifyCfg, tr))
	})

	t.Run("stripped tlog entries rejected", func(t *testing.T) {
		r := require.New(t)
		tampered := mutateBundle(t, func(mm map[string]any) {
			vm := mm["verificationMaterial"].(map[string]any)
			vm["tlogEntries"] = []any{}
		})
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-strip-tlog", tampered), verifyCfg, tr))
	})

	t.Run("wrong digest rejected", func(t *testing.T) {
		r := require.New(t)
		wrong := descruntime.Signature{
			Name: "tamper-wrong-digest",
			Digest: descruntime.Digest{
				HashAlgorithm:          signed.Digest.HashAlgorithm,
				NormalisationAlgorithm: signed.Digest.NormalisationAlgorithm,
				Value:                  "0000000000000000000000000000000000000000000000000000000000000000",
			},
			Signature: signed.Signature,
		}
		err := h.Verify(t.Context(), wrong, verifyCfg, tr)
		r.Error(err)
		r.ErrorContains(err, "verif")
	})

	t.Run("corrupted bundle rejected", func(t *testing.T) {
		r := require.New(t)
		garbage := base64.StdEncoding.EncodeToString([]byte(`{"not":"a valid bundle"}`))
		r.Error(h.Verify(t.Context(), tamperedSignature("tamper-corrupt-bundle", garbage), verifyCfg, tr))
	})
}

func Test_Replay_VerifyWithExplicitTrustedRoot(t *testing.T) {
	h := newHandler(t)
	signed := recordedSignature(t, "verify-explicit-trusted-root-test")

	t.Run("trusted root via credential file path", func(t *testing.T) {
		r := require.New(t)
		r.NoError(h.Verify(t.Context(), signed, verifyConfig(t), trustedRoot(t)))
	})

	t.Run("trusted root via inline credential JSON", func(t *testing.T) {
		r := require.New(t)
		raw, err := os.ReadFile(trustedRootFile)
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), signed, verifyConfig(t), &trustedrootv1.TrustedRoot{
			TrustedRootJSON: string(raw),
		}))
	})

	t.Run("wrong issuer fails with explicit trusted root", func(t *testing.T) {
		r := require.New(t)
		cfg := verifyConfig(t, func(c *v1alpha1.VerifyConfig) {
			c.CertificateOIDCIssuer = "https://wrong-issuer.example.com"
		})
		err := h.Verify(t.Context(), signed, cfg, trustedRoot(t))
		r.Error(err)
		r.ErrorContains(err, "issuer")
	})
}

func Test_Replay_PrivateInfrastructure(t *testing.T) {
	h := newHandler(t)
	signed := recordedSignature(t, "private-infrastructure-test")
	cfg := verifyConfig(t, func(c *v1alpha1.VerifyConfig) {
		c.PrivateInfrastructure = true
	})
	require.New(t).NoError(h.Verify(t.Context(), signed, cfg, trustedRoot(t)))
}
