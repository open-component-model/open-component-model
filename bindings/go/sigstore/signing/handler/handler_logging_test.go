package handler

import (
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/internal/testutil/logtest"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
	oidcv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken/v1alpha1"
)

func findRecord(t *testing.T, records []slog.Record, level slog.Level, substr string) slog.Record {
	t.Helper()
	for _, r := range records {
		if r.Level == level && strings.Contains(r.Message, substr) {
			return r
		}
	}
	t.Fatalf("no %s record matching %q in captured logs (%d records)", level, substr, len(records))
	return slog.Record{}
}

// TestSign_DoesNotLeakOIDCToken verifies that the OIDC token never appears in any log record
// produced by Sign — the token travels through env (SIGSTORE_ID_TOKEN), so neither argv-style
// logs nor the cert-info debug line should ever surface it.
func TestSign_DoesNotLeakOIDCToken(t *testing.T) {
	capt := logtest.Default(t, slog.LevelDebug)

	const secretToken = "super-secret-oidc-token-do-not-log"

	mock := newSignMock(t, fakeBundleJSONWithCert(t, "https://accounts.google.com"))
	h := newWithRunner(mock)

	result, err := h.Sign(t.Context(), testDigest(), testSignConfig(), &oidcv1.OIDCIdentityToken{Token: secretToken})
	require.NoError(t, err)
	require.NotEmpty(t, result.Value)

	require.False(t, capt.Contains(secretToken),
		"OIDC token must not appear in any log record")
}

// TestVerify_LogsConfiguredConstraints verifies that the one Info-level line emitted by Verify
// surfaces the constraints we ask cosign to enforce — this is the actionable signal a user
// running at default log level needs to debug a verification failure.
func TestVerify_LogsConfiguredConstraints(t *testing.T) {
	capt := logtest.Default(t, slog.LevelDebug)

	mock := &execRecorder{}
	h := newWithRunner(mock)

	cfg := testVerifyConfig()
	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: string(v1alpha1.AlgorithmSigstoreV1Alpha1),
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	require.NoError(t, h.Verify(t.Context(), signed, cfg, nil))

	records := capt.Records()
	rec := findRecord(t, records, slog.LevelInfo, "sigstore verify: enforcing identity constraints")
	attrs := logtest.Attrs(rec)
	require.Equal(t, "user@example.com", attrs["certificate_identity"])
	require.Equal(t, "https://accounts.google.com", attrs["certificate_oidc_issuer"])

	require.False(t, capt.Contains(signed.Signature.Value),
		"signature bytes must not appear in logs")
}

// TestVerify_DoesNotLogBundleAcceptedOnFailure ensures we did not regress to logging a success
// line when cosign returned an error. We never want the user to see a "verified" message after
// a failed run.
func TestVerify_DoesNotLogBundleAcceptedOnFailure(t *testing.T) {
	capt := logtest.Default(t, slog.LevelDebug)

	mock := &execRecorder{verifyErr: stubError("verification failed")}
	h := newWithRunner(mock)

	cfg := testVerifyConfig()
	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: string(v1alpha1.AlgorithmSigstoreV1Alpha1),
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	require.Error(t, h.Verify(t.Context(), signed, cfg, nil))

	for _, r := range capt.Records() {
		require.NotContains(t, r.Message, "bundle accepted",
			"verify must not log bundle accepted when cosign returned an error")
		require.NotContains(t, r.Message, "verified",
			"verify must not log a success indicator when cosign returned an error")
	}
}

// stubError is a minimal error type for tests.
type stubError string

func (e stubError) Error() string { return string(e) }
