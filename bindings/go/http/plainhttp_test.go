package http_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// warnHandler records the message of every record at Warn or above.
type warnHandler struct {
	messages *[]string
}

func (h warnHandler) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelWarn {
		*h.messages = append(*h.messages, rec.Message)
	}
	return nil
}

func (h warnHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h warnHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h warnHandler) WithGroup(string) slog.Handler { return h }

// TestPlainHTTPFor pins the shared rule: only the configuration turns plain
// HTTP on, the fallback covers hosts it does not decide, an http:// prefix
// needs plain HTTP, and a cleartext connection warns unless the URL asked for
// http:// itself.
func TestPlainHTTPFor(t *testing.T) {
	const host = "registry.local:5000"
	cfg := func(global, perHost *bool) *httpv1alpha1.Config {
		if global == nil && perHost == nil {
			return nil
		}
		c := &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: global}}
		if perHost != nil {
			c.Hosts = map[string]*httpv1alpha1.HostConfig{
				host: {ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: perHost}},
			}
		}
		return c
	}
	tests := []struct {
		name      string
		global    *bool
		perHost   *bool
		scheme    string
		fallback  bool
		wantPlain bool
		wantErr   bool
		wantWarn  bool
	}{
		{name: "nil config stays HTTPS"},
		{name: "nil config takes the fallback", fallback: true, wantPlain: true, wantWarn: true},
		{name: "global true applies to the host", global: new(true), wantPlain: true, wantWarn: true},
		{name: "per-host false wins over global true", global: new(true), perHost: new(false)},
		{name: "per-host true wins over global false", global: new(false), perHost: new(true), wantPlain: true, wantWarn: true},
		{name: "per-host false wins over the fallback", perHost: new(false), fallback: true},
		{name: "http prefix without plain HTTP is an error", scheme: "http", wantErr: true},
		{name: "http prefix with per-host false is an error", perHost: new(false), scheme: "http", wantErr: true},
		{name: "http prefix on a plain host asked for it and does not warn", perHost: new(true), scheme: "http", wantPlain: true},
		{name: "https prefix on a plain host is downgraded with a warning", perHost: new(true), scheme: "https", wantPlain: true, wantWarn: true},
		{name: "https prefix without plain HTTP stays HTTPS", scheme: "https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var messages []string
			ctx := context.Background()
			logger := slog.New(warnHandler{messages: &messages})
			prev := slog.Default()
			slog.SetDefault(logger)
			t.Cleanup(func() { slog.SetDefault(prev) })

			plain, err := ocmhttp.PlainHTTPFor(ctx, cfg(tt.global, tt.perHost), host, tt.scheme, tt.fallback)
			if tt.wantErr {
				r.ErrorContains(err, "plain HTTP is not enabled")
				r.ErrorContains(err, host, "the error must name the host")
				r.ErrorContains(err, "https://ocm.software/docs/guides/transfer/configure-http/#use-plain-http-for-a-local-registry", "the error must link the guide")
				r.Empty(messages, "an error must not also warn")
				return
			}
			r.NoError(err)
			r.Equal(tt.wantPlain, plain)
			if tt.wantWarn {
				r.Len(messages, 1)
				r.Contains(messages[0], "downgraded to cleartext HTTP")
				return
			}
			r.Empty(messages)
		})
	}
}
