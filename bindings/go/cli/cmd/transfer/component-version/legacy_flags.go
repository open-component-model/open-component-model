package component_version

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Deprecated flags kept for backwards compatibility. They are translated into uploader
// entries of the OCM configuration (see legacyUploaderEntries).
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
const legacyOCIArtifactLocalBlobMatch = `target.type == "OCIRepository" && resource.access.isType("LocalBlob") && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType) && has(resource.access.referenceName)`

func registerLegacyFlags(flags *pflag.FlagSet) {
	flags.Bool(FlagCopyResources, false, "deprecated: copy all resources in the component version")
	enum.VarP(flags, FlagUploadAs, "u", []string{uploadAsLocalBlob, uploadAsOCIArtifact},
		"deprecated: define whether copied resources should be uploaded as OCI artifacts")
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
// flags (last, like the catch-all --copy-resources used to append). cfg is not modified;
// a nil cfg counts as empty.
func withLegacyFlagUploaders(cmd *cobra.Command, cfg *genericv1.Config) (*genericv1.Config, error) {
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
		return cfg, nil
	}

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
