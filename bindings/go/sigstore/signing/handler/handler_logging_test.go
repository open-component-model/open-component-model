package handler

import (
	"context"
	"encoding/base64"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
	oidcv1 "ocm.software/open-component-model/bindings/go/sigstore/spec/credentials/oidcidentitytoken/v1alpha1"
)

// captureHandler is a slog.Handler that records every record passed through it,
// allowing tests to assert which messages the sigstore handler emits and at what level.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *captureHandler) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

// withCapturedSlog swaps the default slog logger for a capturing handler and restores it on cleanup.
func withCapturedSlog(t *testing.T) *captureHandler {
	t.Helper()
	capt := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capt))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return capt
}

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

func recordAttrs(r slog.Record) map[string]any {
	m := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}

// containsValue returns true if any record's message or attribute values include the given string.
// Used to assert that secrets (OIDC tokens, signature bytes) never appear in logs.
func containsValue(records []slog.Record, secret string) bool {
	for _, r := range records {
		if strings.Contains(r.Message, secret) {
			return true
		}
		leaked := false
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), secret) {
				leaked = true
				return false
			}
			return true
		})
		if leaked {
			return true
		}
	}
	return false
}

// TestSign_DoesNotLeakOIDCToken verifies that the OIDC token never appears in any log record
// produced by Sign — the token travels through env (SIGSTORE_ID_TOKEN), so neither argv-style
// logs nor the cert-info debug line should ever surface it.
func TestSign_DoesNotLeakOIDCToken(t *testing.T) {
	capt := withCapturedSlog(t)

	const secretToken = "super-secret-oidc-token-do-not-log"

	mock := newSignMock(t, fakeBundleJSONWithCert(t, "https://accounts.google.com"))
	h := newWithRunner(mock)

	result, err := h.Sign(t.Context(), testDigest(), testSignConfig(), &oidcv1.OIDCIdentityToken{Token: secretToken})
	require.NoError(t, err)
	require.NotEmpty(t, result.Value)

	require.False(t, containsValue(capt.snapshot(), secretToken),
		"OIDC token must not appear in any log record")
}

// TestVerify_LogsConfiguredConstraints verifies that the one Info-level line emitted by Verify
// surfaces the constraints we ask cosign to enforce — this is the actionable signal a user
// running at default log level needs to debug a verification failure.
func TestVerify_LogsConfiguredConstraints(t *testing.T) {
	capt := withCapturedSlog(t)

	mock := &execRecorder{}
	h := newWithRunner(mock)

	cfg := testVerifyConfig()
	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	require.NoError(t, h.Verify(t.Context(), signed, cfg, nil))

	records := capt.snapshot()
	rec := findRecord(t, records, slog.LevelInfo, "sigstore verify: enforcing identity constraints")
	attrs := recordAttrs(rec)
	require.Equal(t, "user@example.com", attrs["certificate_identity"])
	require.Equal(t, "https://accounts.google.com", attrs["certificate_oidc_issuer"])

	require.False(t, containsValue(records, signed.Signature.Value),
		"signature bytes must not appear in logs")
}

// TestVerify_DoesNotLogBundleAcceptedOnFailure ensures we did not regress to logging a success
// line when cosign returned an error. We never want the user to see a "verified" message after
// a failed run.
func TestVerify_DoesNotLogBundleAcceptedOnFailure(t *testing.T) {
	capt := withCapturedSlog(t)

	mock := &execRecorder{verifyErr: errStub("verification failed")}
	h := newWithRunner(mock)

	cfg := testVerifyConfig()
	bundleJSON := fakeBundleJSON(t)
	signed := descruntime.Signature{
		Name:   "test-sig",
		Digest: testDigest(),
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmSigstore,
			MediaType: v1alpha1.MediaTypeSigstoreBundle,
			Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		},
	}

	require.Error(t, h.Verify(t.Context(), signed, cfg, nil))

	for _, r := range capt.snapshot() {
		require.NotContains(t, r.Message, "bundle accepted",
			"verify must not log bundle accepted when cosign returned an error")
		require.NotContains(t, r.Message, "verified",
			"verify must not log a success indicator when cosign returned an error")
	}
}

// errStub is a minimal error type for tests.
type errStub string

func (e errStub) Error() string { return string(e) }
