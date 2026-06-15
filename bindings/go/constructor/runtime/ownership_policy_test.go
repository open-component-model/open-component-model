package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
)

// TestConvertV1Resource_OwnershipPolicyRoundTrip covers the options.ownershipPolicy
// mapping (ADR 0016) between the v1 and runtime Resource: an explicitly set policy
// survives the round-trip, and an absent options block stays absent.
func TestConvertV1Resource_OwnershipPolicyRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		options     *v1.ResourceOptions
		wantRuntime OwnershipPolicy
		wantBack    *v1.ResourceOptions
	}{
		{
			name:        "Always",
			options:     &v1.ResourceOptions{OwnershipPolicy: v1.OwnershipPolicyAlways},
			wantRuntime: OwnershipPolicyAlways,
			wantBack:    &v1.ResourceOptions{OwnershipPolicy: v1.OwnershipPolicyAlways},
		},
		{
			name:        "Never (explicit)",
			options:     &v1.ResourceOptions{OwnershipPolicy: v1.OwnershipPolicyNever},
			wantRuntime: OwnershipPolicyNever,
			wantBack:    &v1.ResourceOptions{OwnershipPolicy: v1.OwnershipPolicyNever},
		},
		{
			name:        "unset (nil options)",
			options:     nil,
			wantRuntime: "",
			wantBack:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := &v1.Resource{
				ElementMeta: v1.ElementMeta{ObjectMeta: v1.ObjectMeta{Name: "image", Version: "1.0.0"}},
				Type:        "ociArtifact",
				Options:     tt.options,
			}

			rt := ConvertFromV1Resource(in)
			require.Equal(t, tt.wantRuntime, rt.Options.OwnershipPolicy, "v1 -> runtime policy")

			back, err := ConvertToV1Resource(&rt)
			require.NoError(t, err)
			require.Equal(t, tt.wantBack, back.Options, "runtime -> v1 options block (omitted unless Always)")
		})
	}
}
