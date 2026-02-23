package setup

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

// HTTPConfig sets up HTTP client configuration entity.
func HTTPConfig(cmd *cobra.Command) {
	ocmCtx := ocmctx.FromContext(cmd.Context())
	cfg := ocmCtx.Configuration()
	var httpCfg *httpv1alpha1.Config
	if cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize http config")
		httpCfg = &httpv1alpha1.Config{}
	} else {
		if _httpCfg, err := httpv1alpha1.LookupConfig(cfg); err != nil {
			slog.WarnContext(cmd.Context(), "could not get http configuration, using defaults", slog.String("error", err.Error()))
			httpCfg = &httpv1alpha1.Config{}
		} else {
			httpCfg = _httpCfg
		}
	}

	// CLI flags take precedence over the config file values.
	httpCfg.Timeout = overrideFromFlag(cmd, ocmcmd.TimeoutFlag, httpCfg.Timeout)
	httpCfg.TCPDialTimeout = overrideFromFlag(cmd, ocmcmd.TCPDialTimeoutFlag, httpCfg.TCPDialTimeout)
	httpCfg.TCPKeepAlive = overrideFromFlag(cmd, ocmcmd.TCPKeepAliveFlag, httpCfg.TCPKeepAlive)
	httpCfg.TLSHandshakeTimeout = overrideFromFlag(cmd, ocmcmd.TLSHandshakeTimeoutFlag, httpCfg.TLSHandshakeTimeout)
	httpCfg.ResponseHeaderTimeout = overrideFromFlag(cmd, ocmcmd.ResponseHeaderTimeoutFlag, httpCfg.ResponseHeaderTimeout)
	httpCfg.IdleConnTimeout = overrideFromFlag(cmd, ocmcmd.IdleConnTimeoutFlag, httpCfg.IdleConnTimeout)
	httpCfg.RetryMinWait = overrideFromFlag(cmd, ocmcmd.RetryMinWaitFlag, httpCfg.RetryMinWait)
	httpCfg.RetryMaxWait = overrideFromFlag(cmd, ocmcmd.RetryMaxWaitFlag, httpCfg.RetryMaxWait)
	httpCfg.RetryMaxRetry = overrideIntFromFlag(cmd, ocmcmd.RetryMaxRetryFlag, httpCfg.RetryMaxRetry)

	ctx := ocmctx.WithHTTPConfig(cmd.Context(), httpCfg)
	cmd.SetContext(ctx)
}

// overrideFromFlag returns an overridden timeout from a CLI flag if it was explicitly set,
// otherwise returns the original value unchanged.
func overrideFromFlag(cmd *cobra.Command, flagName string, current *httpv1alpha1.Timeout) *httpv1alpha1.Timeout {
	flag := cmd.Flags().Lookup(flagName)
	if flag == nil || !flag.Changed {
		return current
	}

	ctx := cmd.Context()

	d, err := time.ParseDuration(flag.Value.String())
	if err != nil {
		slog.WarnContext(ctx, "failed to parse flag value",
			slog.String("flag", flagName),
			slog.String("error", err.Error()))
		return current
	}

	original := "<nil>"
	if current != nil {
		original = current.String()
	}

	slog.DebugContext(ctx, "overriding timeout from CLI flag",
		slog.String("flag", flagName),
		slog.String("original", original),
		slog.String("new", httpv1alpha1.NewTimeout(d).String()))

	return httpv1alpha1.NewTimeout(d)
}

// overrideIntFromFlag returns an overridden int value from a CLI flag if it was explicitly set,
// otherwise returns the original value unchanged.
func overrideIntFromFlag(cmd *cobra.Command, flagName string, current *int) *int {
	flag := cmd.Flags().Lookup(flagName)
	if flag == nil || !flag.Changed {
		return current
	}

	ctx := cmd.Context()

	v, err := strconv.Atoi(flag.Value.String())
	if err != nil {
		slog.WarnContext(ctx, "failed to parse flag value",
			slog.String("flag", flagName),
			slog.String("error", err.Error()))
		return current
	}

	original := "<nil>"
	if current != nil {
		original = strconv.Itoa(*current)
	}

	slog.DebugContext(ctx, "overriding value from CLI flag",
		slog.String("flag", flagName),
		slog.String("original", original),
		slog.Int("new", v))

	return &v
}
