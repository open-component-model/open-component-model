package owner

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	componentversion "ocm.software/open-component-model/cli/cmd/get/component-version"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/render"
)

const (
	FlagOutput = "output"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "owner {ref}",
		Short: "Get owning component version(s) of an artifact",
		Long: `Get the owning component version(s) of an artifact.

The access type is declared by prefixing the reference with an OCM type
selector, the same convention used by 'ocm get component-version' and
'ocm transfer'. Today only 'oci::' is recognized; once the resource registry
exposes per-backend reference parsers, this command will auto-detect the
access type and gain support for Helm chart and other backends.

'-o json' short-circuits the component-version resolution step and emits the
raw owner-lookup payload directly.`,
		Example:           `  ocm get owner oci::ghcr.io/acme/ocm/component-descriptors/ocm.software/app@sha256:abc...`,
		Args:              cobra.ExactArgs(1),
		RunE:              GetOwner,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String()}, "output format")

	return cmd
}

func GetOwner(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ref := args[0]

	ocmContext := ocmctx.FromContext(ctx)
	pluginManager := ocmContext.PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}
	credentialGraph := ocmContext.CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	// Until the resource registry exposes a generic ParseResourceAccess
	// dispatcher (with each backend contributing its own parser), pin
	// access-type detection to the `oci::` OCM type prefix — same convention
	// `get cv` and `transfer cv` use for their positional refs.
	const ociPrefix = "oci::"
	if !strings.HasPrefix(ref, ociPrefix) {
		return fmt.Errorf("unsupported reference %q: only %s references are supported", ref, ociPrefix)
	}
	imageRef := strings.TrimPrefix(ref, ociPrefix)
	access := &accessv1.OCIImage{ImageReference: imageRef}
	res := &descruntime.Resource{Access: access}

	var creds runtime.Typed
	identity, identityErr := pluginManager.ResourcePluginRegistry.GetResourceCredentialConsumerIdentity(ctx, res)
	if identityErr != nil {
		// A failure here is not fatal — the access type may legitimately
		// require no credentials. Log at debug so a misconfigured plugin or a
		// crashed identity computation can still be traced.
		slog.DebugContext(ctx, "no credential consumer identity available; proceeding without credentials", "ref", ref, "error", identityErr)
	} else {
		creds, err = credentialGraph.Resolve(ctx, identity)
		if err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return fmt.Errorf("resolving credentials: %w", err)
		}
	}

	owners, err := pluginManager.ResourcePluginRegistry.LookupResourceOwners(ctx, res, creds)
	if err != nil {
		return fmt.Errorf("looking up owners for %q: %w", ref, err)
	}
	if len(owners) == 0 {
		return writeNoOwners(cmd, ref)
	}

	// JSON output is the raw ownership referrer payload — callers asking for
	// JSON typically want the lookup result itself, not the resolved owning
	// component descriptors. Skip the cv resolution step entirely.
	if output == render.OutputFormatJSON.String() {
		return writeOwnersJSON(cmd, owners)
	}

	repoBase, err := ocmRepositoryBase(imageRef)
	if err != nil {
		return err
	}

	cvCmd, err := newCvCommand(cmd, output)
	if err != nil {
		return err
	}
	for _, cvRef := range ownerRefs(repoBase, owners) {
		if err := cvCmd.RunE(cvCmd, []string{cvRef}); err != nil {
			return fmt.Errorf("getting owning component version %q: %w", cvRef, err)
		}
	}
	return nil
}

// newCvCommand builds the `get cv` cobra command configured to inherit
// context and IO from parent and render in the given output format. Hoisted
// out of the per-owner loop in GetOwner so flag wiring and the cv command
// tree are constructed once; the caller invokes the returned command's RunE
// per owner with the synthetic component reference.
func newCvCommand(parent *cobra.Command, output string) (*cobra.Command, error) {
	cvCmd := componentversion.New()
	cvCmd.SetContext(parent.Context())
	cvCmd.SetOut(parent.OutOrStdout())
	cvCmd.SetErr(parent.ErrOrStderr())
	cvCmd.SilenceUsage = true
	cvCmd.SilenceErrors = true
	if err := cvCmd.Flags().Set(componentversion.FlagOutput, output); err != nil {
		return nil, fmt.Errorf("propagating output flag to get cv: %w", err)
	}
	return cvCmd, nil
}

// ownerRefs builds the deduplicated `get cv` references for each owning
// (name, version) pair. The `//` after repoBase relies on compref's empty
// prefix falling back to `component-descriptors`.
func ownerRefs(repoBase string, owners []repository.ResourceOwner) []string {
	refs := make([]string, 0, len(owners))
	seen := make(map[string]struct{}, len(owners))
	for _, o := range owners {
		ref := repoBase + "//" + o.ComponentName + ":" + o.ComponentVersion
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

// writeOwnersJSON marshals the raw owner-lookup payloads to the command's
// stdout. Used for `-o json` to surface the lookup result without doing an
// additional component-version resolution. Mirrors the MarshalIndent + Write
// idiom used by the cv list renderer in cli/internal/render/graph/list.
func writeOwnersJSON(cmd *cobra.Command, owners []repository.ResourceOwner) error {
	data, err := json.MarshalIndent(owners, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling owners to JSON failed: %w", err)
	}
	data = append(data, '\n')
	if _, err := cmd.OutOrStdout().Write(data); err != nil {
		return fmt.Errorf("writing JSON data to writer failed: %w", err)
	}
	return nil
}

// writeNoOwners emits the message used when the ownership lookup returns an
// empty set. Extracted from GetOwner so the message format is lockable in a
// unit test without standing up the plugin manager.
func writeNoOwners(cmd *cobra.Command, ref string) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "no owning component version found for %s\n", ref); err != nil {
		return fmt.Errorf("writing no-owners message: %w", err)
	}
	return nil
}

// ocmRepositoryBase extracts the OCM repository location from an OCI image
// reference. OCM stores a by-value artifact and its component descriptor side
// by side under the "component-descriptors" prefix, so the registry and
// sub-path preceding that prefix also host the owning component versions.
func ocmRepositoryBase(imageRef string) (string, error) {
	ref, err := looseref.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("parsing image reference %q: %w", imageRef, err)
	}
	repoPath := ref.Repository
	if ref.Registry != "" {
		repoPath = ref.Registry + "/" + ref.Repository
	}
	if ref.Scheme != "" {
		// Preserve the scheme (http/oci) so the derived component reference
		// talks to the registry the same way the image ref did. compref
		// requires an explicit oci:: type prefix when a scheme is present.
		repoPath = "oci::" + ref.Scheme + "://" + repoPath
	}
	marker := "/" + compref.DefaultPrefix + "/"
	// Find the rightmost match so a registry path that itself contains the
	// `component-descriptors` string as a substring of a different segment
	// (e.g. `mirrors/component-descriptors-archive/component-descriptors/...`)
	// doesn't truncate at the false-positive prefix.
	idx := strings.LastIndex(repoPath, marker)
	if idx < 0 {
		return "", fmt.Errorf("image reference %q is not in the OCM %q layout; cannot locate the owning component version", imageRef, compref.DefaultPrefix)
	}
	return repoPath[:idx], nil
}
