package replay_test

// TestRecordFixtures regenerates the committed replay fixtures by signing a
// fixed digest against the live scaffolding and copying the trusted root. It is
// skipped unless RECORD_FIXTURES is set with the sigstore env sourced; use
// `task bindings/go/sigstore/integration:fixtures:record`.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
	oidcv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken/v1alpha1"
)

func TestRecordFixtures(t *testing.T) {
	if os.Getenv(recordEnv) == "" {
		t.Skip("set RECORD_FIXTURES=1 with the sigstore env sourced to re-record (task fixtures:record)")
	}
	r := require.New(t)

	env := map[string]string{
		"SIGSTORE_SIGNING_CONFIG": os.Getenv("SIGSTORE_SIGNING_CONFIG"),
		"SIGSTORE_OIDC_TOKEN":     os.Getenv("SIGSTORE_OIDC_TOKEN"),
		"SIGSTORE_OIDC_ISSUER":    os.Getenv("SIGSTORE_OIDC_ISSUER"),
		"SIGSTORE_OIDC_IDENTITY":  os.Getenv("SIGSTORE_OIDC_IDENTITY"),
		"SIGSTORE_TRUSTED_ROOT":   os.Getenv("SIGSTORE_TRUSTED_ROOT"),
	}
	for k, v := range env {
		r.NotEmptyf(v, "%s must be set (source the sigstore env)", k)
	}

	// Fixed digest so the recorded bundle and the replay refer to the same message.
	sum := sha256.Sum256([]byte("ocm-sigstore-replay-fixture-v1"))
	digest := descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "jsonNormalisation/v2",
		Value:                  hex.EncodeToString(sum[:]),
	}

	signCfg := &v1alpha1.SignConfig{SigningConfig: env["SIGSTORE_SIGNING_CONFIG"]}
	signCfg.SetType(runtime.NewVersionedType(v1alpha1.SignConfigType, v1alpha1.Version))

	h := handler.New()
	sigInfo, err := h.Sign(t.Context(), digest, signCfg, &oidcv1.OIDCIdentityToken{Token: env["SIGSTORE_OIDC_TOKEN"]})
	r.NoError(err, "live sign to record fixture")
	r.NotEmpty(sigInfo.Value)

	r.NoError(os.MkdirAll("testdata", 0o755))
	r.NoError(os.WriteFile(bundleFile, []byte(sigInfo.Value), 0o644))

	trustedRootBytes, err := os.ReadFile(env["SIGSTORE_TRUSTED_ROOT"])
	r.NoError(err)
	r.NoError(os.WriteFile(trustedRootFile, trustedRootBytes, 0o644))

	m := meta{
		HashAlgorithm:          digest.HashAlgorithm,
		NormalisationAlgorithm: digest.NormalisationAlgorithm,
		DigestValue:            digest.Value,
		Algorithm:              sigInfo.Algorithm,
		MediaType:              sigInfo.MediaType,
		OIDCIssuer:             env["SIGSTORE_OIDC_ISSUER"],
		OIDCIdentity:           env["SIGSTORE_OIDC_IDENTITY"],
	}
	metaBytes, err := json.Marshal(m, runtime.JSONOptions, jsontext.WithIndent("  "))
	r.NoError(err)
	r.NoError(os.WriteFile(metaFile, append(metaBytes, '\n'), 0o644))

	t.Logf("recorded fixture: digest=%s issuer=%s identity=%s algorithm=%s", digest.Value, m.OIDCIssuer, m.OIDCIdentity, m.Algorithm)
}
