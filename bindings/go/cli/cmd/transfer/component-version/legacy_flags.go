package component_version

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
)

// Deprecated flags kept for backwards compatibility. They are hidden from the help and
// translated into --uploader values (see legacyUploaderValues).
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
	flags.Bool(FlagCopyResources, false, "copy all resources in the component version")
	enum.VarP(flags, FlagUploadAs, "u", []string{uploadAsLocalBlob, uploadAsOCIArtifact},
		"define whether copied resources should be uploaded as OCI artifacts (instead of local blob resources)")
	// MarkDeprecated only fails for unknown flags, which are registered right above.
	_ = flags.MarkDeprecated(FlagCopyResources,
		"use --uploader localblob instead (--copy-resources=false has no effect: it cannot remove configured uploaders)")
	_ = flags.MarkDeprecated(FlagUploadAs,
		"use --uploader instead: with --copy-resources, --upload-as ociArtifact is --uploader oci --uploader localblob; alone, it uploads only OCI-manifest local blobs (see \"Migrate from --upload-as to Uploader Configurations\" on ocm.software)")
}

// legacyUploaderValues translates the deprecated flags into --uploader values that
// reproduce their former behavior:
//
//	--copy-resources                           localblob
//	--copy-resources --upload-as ociArtifact   oci, localblob
//	--upload-as ociArtifact                    oci restricted to OCI-manifest local blobs
//	--upload-as localBlob, --copy-resources=false, neither   nothing
//
// --copy-resources=false used to override a configured copy mode; uploaders from the OCM
// configuration cannot be removed by a flag, so it translates to nothing.
func legacyUploaderValues(copyResources bool, uploadAs string) []string {
	switch {
	case copyResources && uploadAs == uploadAsOCIArtifact:
		return []string{"oci", "localblob"}
	case copyResources:
		return []string{"localblob"}
	case uploadAs == uploadAsOCIArtifact:
		return []string{"oci=" + legacyOCIArtifactLocalBlobMatch}
	default:
		return nil
	}
}

// legacyUploaderValuesFromFlags reads the deprecated flags of cmd and translates them with
// legacyUploaderValues.
func legacyUploaderValuesFromFlags(cmd *cobra.Command) ([]string, error) {
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
	return legacyUploaderValues(copyResources, uploadAs), nil
}
