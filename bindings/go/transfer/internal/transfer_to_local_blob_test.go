package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	s3accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// TestTransferToLocalBlob_RejectsInvalidInput verifies that input the schema cannot rule out
// is rejected before the source or the target is contacted: the transformer has neither.
func TestTransferToLocalBlob_RejectsInvalidInput(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
	transformer := &TransferToLocalBlob{Scheme: scheme}

	wget := func(url, verb string) wgetaccessv1.Wget {
		return wgetaccessv1.Wget{Type: runtime.NewVersionedType("Wget", wgetaccessv1.Version), URL: url, Verb: verb}
	}
	target := &descriptorv2.Resource{ElementMeta: descriptorv2.ElementMeta{ObjectMeta: descriptorv2.ObjectMeta{Name: "data", Version: "1.0.0"}}}
	wgetStep := func(access wgetaccessv1.Wget, component string, targetResource *descriptorv2.Resource) runtime.Typed {
		return &uploadv1alpha1.TransferWgetToCTFLocalBlob{
			Type: uploadv1alpha1.TransferWgetToCTFLocalBlobV1alpha1,
			Spec: &uploadv1alpha1.TransferWgetToCTFLocalBlobSpec{Access: access, Component: component, Version: "1.0.0", TargetResource: targetResource},
		}
	}

	tests := []struct {
		name    string
		step    runtime.Typed
		wantErr string
	}{
		{"wget POST", wgetStep(wget("https://example.com/a", "POST"), "ocm.software/comp", target), `verb "POST" cannot be streamed`},
		{"wget unsupported scheme", wgetStep(wget("ftp://example.com/a", ""), "ocm.software/comp", target), "http or https"},
		{"wget without url", wgetStep(wget("", ""), "ocm.software/comp", target), "url is required"},
		{"missing target resource", wgetStep(wget("https://example.com/a", ""), "ocm.software/comp", nil), "target resource is required"},
		{"missing component", wgetStep(wget("https://example.com/a", ""), "", target), "component name is required"},
		{"missing spec", &uploadv1alpha1.TransferS3ToOCILocalBlob{Type: uploadv1alpha1.TransferS3ToOCILocalBlobV1alpha1}, "spec is required"},
		{"s3 without bucket", &uploadv1alpha1.TransferS3ToCTFLocalBlob{
			Type: uploadv1alpha1.TransferS3ToCTFLocalBlobV1alpha1,
			Spec: &uploadv1alpha1.TransferS3ToCTFLocalBlobSpec{
				Access:    s3accessv2.S3{Type: s3accessspec.V2VersionedType, ObjectKey: "key"},
				Component: "ocm.software/comp", Version: "1.0.0", TargetResource: target,
			},
		}, "invalid source access"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := transformer.Transform(t.Context(), tc.step)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
