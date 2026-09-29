package component_version

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Deprecated flags kept for backwards compatibility. They are translated into uploader
// entries of the OCM configuration (see legacyUploaderEntries); using them logs the
// generated configuration so it can replace the flags.
//
// TODO(legacy-flags): remove this file and every TODO(legacy-flags) test with the flags.
const (
	FlagCopyResources = "copy-resources"
	FlagUploadAs      = "upload-as"

	uploadAsLocalBlob   = "localBlob"
	uploadAsOCIArtifact = "ociArtifact"
)

// legacyOCIArtifactLocalBlobMatch is the match reproducing --upload-as ociArtifact without
// --copy-resources: only OCI-manifest local blobs with a referenceName became OCI artifacts,
// and only on OCI registry targets; OCI image and Helm references stayed by reference.
const legacyOCIArtifactLocalBlobMatch = `target.type == "OCIRepository" && resource.access.isType("LocalBlob") && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)`

func registerLegacyFlags(flags *pflag.FlagSet) {
	flags.Bool(FlagCopyResources, false, "deprecated: copy all resources in the component version (logs the equivalent OCM configuration to use instead)")
	enum.VarP(flags, FlagUploadAs, "u", []string{uploadAsLocalBlob, uploadAsOCIArtifact},
		"deprecated: define whether copied resources should be uploaded as OCI artifacts (logs the equivalent OCM configuration to use instead)")
}

// legacyUploaderEntries translates the deprecated flags into uploader configuration
// entries that reproduce their former behavior:
//
//	--copy-resources                           localblob
//	--copy-resources --upload-as ociArtifact   oci, localblob
//	--upload-as ociArtifact                    oci restricted to OCI-manifest local blobs
//	--upload-as localBlob, --copy-resources=false, neither   nothing
//
// --copy-resources=false used to override a configured copy mode; uploaders from the OCM
// configuration cannot be removed by a flag, so it translates to nothing.
func legacyUploaderEntries(copyResources bool, uploadAs string) []map[string]any {
	oci := map[string]any{"type": runtime.NewVersionedType(transferv1alpha1.OCIUploaderConfigType, transferv1alpha1.Version).String()}
	localBlob := map[string]any{"type": runtime.NewVersionedType(transferv1alpha1.LocalBlobUploaderConfigType, transferv1alpha1.Version).String()}
	switch {
	case copyResources && uploadAs == uploadAsOCIArtifact:
		return []map[string]any{oci, localBlob}
	case copyResources:
		return []map[string]any{localBlob}
	case uploadAs == uploadAsOCIArtifact:
		oci["match"] = legacyOCIArtifactLocalBlobMatch
		return []map[string]any{oci}
	default:
		return nil
	}
}

// withLegacyFlagUploaders returns a copy of cfg whose configurations are cfg's entries
// followed by the entries translated from the deprecated --copy-resources and --upload-as
// flags (last, like the catch-all --copy-resources used to append). When flags translate
// to entries, it logs a warning with the generated configuration to use instead. cfg is
// not modified; a nil cfg counts as empty.
func withLegacyFlagUploaders(ctx context.Context, cmd *cobra.Command, cfg *genericv1.Config) (*genericv1.Config, error) {
	copyResources, err := cmd.Flags().GetBool(FlagCopyResources)
	if err != nil {
		return nil, fmt.Errorf("getting %s flag failed: %w", FlagCopyResources, err)
	}
	uploadAs := ""
	if cmd.Flags().Changed(FlagUploadAs) {
		if uploadAs, err = enum.Get(cmd.Flags(), FlagUploadAs); err != nil {
			return nil, fmt.Errorf("getting %s flag failed: %w", FlagUploadAs, err)
		}
	}
	entries := legacyUploaderEntries(copyResources, uploadAs)
	if len(entries) == 0 {
		if cmd.Flags().Changed(FlagCopyResources) || cmd.Flags().Changed(FlagUploadAs) {
			slog.WarnContext(ctx, fmt.Sprintf("--%s and --%s are deprecated and have no effect with these values; remove them", FlagCopyResources, FlagUploadAs))
		}
		return cfg, nil
	}

	// Without HTML escaping, && in match expressions stays readable for copy and paste.
	var generated strings.Builder
	enc := json.NewEncoder(&generated)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{
		"type":           runtime.NewVersionedType(genericv1.ConfigType, genericv1.Version).String(),
		"configurations": entries,
	}); err != nil {
		return nil, err
	}
	slog.WarnContext(ctx, fmt.Sprintf("--%s and --%s are deprecated: replace them with the OCM configuration in the config attribute (JSON is valid YAML), passed via --config or added to your existing configuration", FlagCopyResources, FlagUploadAs),
		"config", strings.TrimSpace(generated.String()))

	out := &genericv1.Config{}
	if cfg != nil {
		out.Type = cfg.Type
		out.Configurations = slices.Clone(cfg.Configurations)
	}
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		t, err := runtime.TypeFromString(e["type"].(string))
		if err != nil {
			return nil, err
		}
		out.Configurations = append(out.Configurations, &runtime.Raw{Type: t, Data: data})
	}
	return out, nil
}
