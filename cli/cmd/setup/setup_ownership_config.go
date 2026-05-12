package setup

import (
	"log/slog"

	"github.com/spf13/cobra"

	ownershipv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/ownership/v1alpha1/spec"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

// OwnershipConfig resolves the ownership referrer configuration from the
// central OCM configuration and stores it on the command context, from where
// it can be retrieved via [ocmctx.Context.OwnershipConfig].
//
// A missing or malformed ownership configuration is not fatal: the context is
// left without one, which the OCI provider treats as "never emit referrers".
func OwnershipConfig(cmd *cobra.Command) {
	cfg := ocmctx.FromContext(cmd.Context()).Configuration()
	if cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize ownership config")
		return
	}

	ownershipConfig, err := ownershipv1alpha1.Lookup(cfg)
	if err != nil {
		slog.DebugContext(cmd.Context(), "could not get ownership configuration", slog.String("error", err.Error()))
		return
	}

	cmd.SetContext(ocmctx.WithOwnershipConfig(cmd.Context(), ownershipConfig))
}
