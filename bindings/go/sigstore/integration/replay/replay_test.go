package replay_test

// Offline replay of the sigstore verification tests. These do NOT need the
// scaffolding cluster: they verify a bundle recorded once (see record_test.go,
// `task fixtures:record`) against a committed trusted root, using the real
// handler and the pinned cosign binary. Only the live sign path
// (sigstore/integration) needs the cluster.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
	trustedrootv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/trustedroot/v1alpha1"
)

const (
	bundleFile      = "testdata/bundle.b64"
	metaFile        = "testdata/meta.json"
	trustedRootFile = "testdata/trusted_root.json"
	recordEnv       = "RECORD_FIXTURES"
)

// meta is the committed description of the recorded bundle. Only public material
// (the digest it was signed over, the issuer/identity the scaffolding Fulcio put
// in the certificate, and the envelope type) is stored; the bundle itself lives
// in bundle.b64 and the trust material in trusted_root.json.
type meta struct {
	HashAlgorithm          string `json:"hashAlgorithm"`
	NormalisationAlgorithm string `json:"normalisationAlgorithm"`
	DigestValue            string `json:"digestValue"`
	Algorithm              string `json:"algorithm"`
	MediaType              string `json:"mediaType"`
	OIDCIssuer             string `json:"oidcIssuer"`
	OIDCIdentity           string `json:"oidcIdentity"`
}

func loadMeta(t *testing.T) meta {
	t.Helper()
	raw, err := os.ReadFile(metaFile)
	if err != nil {
		t.Fatalf("read fixture meta (run `task bindings/go/sigstore/integration:fixtures:record`): %v", err)
	}
	var m meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal fixture meta: %v", err)
	}
	return m
}

// recordedSignature reconstructs the descriptor Signature from the committed
// bundle and meta, using the given name so each test gets a distinct signature.
func recordedSignature(t *testing.T, name string) descruntime.Signature {
	t.Helper()
	m := loadMeta(t)
	raw, err := os.ReadFile(bundleFile)
	if err != nil {
		t.Fatalf("read fixture bundle (run `task bindings/go/sigstore/integration:fixtures:record`): %v", err)
	}
	return descruntime.Signature{
		Name: name,
		Digest: descruntime.Digest{
			HashAlgorithm:          m.HashAlgorithm,
			NormalisationAlgorithm: m.NormalisationAlgorithm,
			Value:                  m.DigestValue,
		},
		Signature: descruntime.SignatureInfo{
			Algorithm: m.Algorithm,
			MediaType: m.MediaType,
			Value:     string(raw),
		},
	}
}

func newHandler(t *testing.T) *handler.Handler {
	t.Helper()
	return handler.New()
}

// trustedRoot returns the committed trusted root as a credential. The replayed
// verifications pass it explicitly (rather than relying on cosign's ambient TUF
// cache) so they run fully offline; the live lane still covers the TUF path.
func trustedRoot(t *testing.T) *trustedrootv1.TrustedRoot {
	t.Helper()
	abs, err := filepath.Abs(trustedRootFile)
	if err != nil {
		t.Fatalf("resolve trusted root path: %v", err)
	}
	return &trustedrootv1.TrustedRoot{TrustedRootJSONFile: abs}
}

func verifyConfig(t *testing.T, opts ...func(*v1alpha1.VerifyConfig)) *v1alpha1.VerifyConfig {
	t.Helper()
	m := loadMeta(t)
	cfg := &v1alpha1.VerifyConfig{
		CertificateOIDCIssuer: m.OIDCIssuer,
		CertificateIdentity:   m.OIDCIdentity,
	}
	for _, o := range opts {
		o(cfg)
	}
	cfg.SetType(runtime.NewVersionedType(v1alpha1.VerifyConfigType, v1alpha1.Version))
	return cfg
}
