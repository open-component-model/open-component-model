package setup

import (
	"log/slog"
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
	overrideFromFlag(cmd, ocmcmd.TimeoutFlag, httpCfg.Timeout)
	overrideFromFlag(cmd, ocmcmd.TCPDialTimeoutFlag, httpCfg.TCPDialTimeout)
	overrideFromFlag(cmd, ocmcmd.TCPKeepAliveFlag, httpCfg.TCPKeepAlive)
	overrideFromFlag(cmd, ocmcmd.TLSHandshakeTimeoutFlag, httpCfg.TLSHandshakeTimeout)
	overrideFromFlag(cmd, ocmcmd.ResponseHeaderTimeoutFlag, httpCfg.ResponseHeaderTimeout)
	overrideFromFlag(cmd, ocmcmd.IdleConnTimeoutFlag, httpCfg.IdleConnTimeout)

	ctx := ocmctx.WithHTTPConfig(cmd.Context(), httpCfg)
	cmd.SetContext(ctx)
}

// overrideFromFlag overrides a timeout field from a CLI flag if it was explicitly set.
func overrideFromFlag(cmd *cobra.Command, flagName string, target *httpv1alpha1.Timeout) {
	flag := cmd.Flags().Lookup(flagName)
	if flag == nil || !flag.Changed {
		return
	}

	ctx := cmd.Context()

	d, err := time.ParseDuration(flag.Value.String())
	if err != nil {
		slog.WarnContext(ctx, "failed to parse flag value",
			slog.String("flag", flagName),
			slog.String("error", err.Error()))
		return
	}

	original := "<nil>"
	if target != nil {
		original = target.String()
	}

	slog.DebugContext(ctx, "overriding timeout from CLI flag",
		slog.String("flag", flagName),
		slog.String("original", original),
		slog.String("new", httpv1alpha1.NewTimeout(d).String()))

	target = httpv1alpha1.NewTimeout(d)
}
