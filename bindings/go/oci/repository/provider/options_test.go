package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ownershipv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/ownership/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/oci"
	repoSpec "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestResolveOwnershipReferrerPolicy(t *testing.T) {
	rawSpec := func(t *testing.T, jsonStr string) *runtime.Raw {
		t.Helper()
		raw := &runtime.Raw{}
		require.NoError(t, json.Unmarshal([]byte(jsonStr), raw))
		return raw
	}

	makeCfg := func(t *testing.T, policy ownershipv1alpha1.Policy, repos ...*ownershipv1alpha1.RepositoryPolicy) *ownershipv1alpha1.Config {
		t.Helper()
		return &ownershipv1alpha1.Config{Policy: policy, Repositories: repos}
	}

	targetOCIGhcrMyOrg := rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`)
	targetOCIGhcrEmbeddedPath := rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io/my-org/components"}`)
	targetUnknownType := rawSpec(t, `{"type":"UnregisteredRepository/v1"}`)

	tests := []struct {
		name    string
		cfg     *ownershipv1alpha1.Config
		target  *runtime.Raw
		want    oci.OwnershipReferrerPolicy
		wantErr bool
	}{
		{
			name:   "nil config → never",
			cfg:    nil,
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name:   "empty config → never",
			cfg:    &ownershipv1alpha1.Config{},
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name:   "top-level AddIfSupported, no overrides",
			cfg:    makeCfg(t, ownershipv1alpha1.PolicyAddIfSupported),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name:   "top-level Never, no overrides",
			cfg:    makeCfg(t, ownershipv1alpha1.PolicyNever),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name:    "unsupported top-level policy → error",
			cfg:     makeCfg(t, "Bogus"),
			target:  targetOCIGhcrMyOrg,
			wantErr: true,
		},
		{
			name: "type-only override matches any spec of same type",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "specific override beats top-level fallback",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "exact match wins over wildcard (exact listed first)",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever,
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
					Policy:     ownershipv1alpha1.PolicyAddIfSupported,
				},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1"}`),
					Policy:     ownershipv1alpha1.PolicyNever,
				},
			),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "exact match wins over wildcard (exact listed last)",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever,
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1"}`),
					Policy:     ownershipv1alpha1.PolicyNever,
				},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
					Policy:     ownershipv1alpha1.PolicyNever,
				},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
					Policy:     ownershipv1alpha1.PolicyAddIfSupported,
				},
			),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "first wildcard wins when no exact match",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever,
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
					Policy:     ownershipv1alpha1.PolicyAddIfSupported,
				},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1"}`),
					Policy:     ownershipv1alpha1.PolicyNever,
				},
			),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "non-matching override falls through to top-level",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyAddIfSupported, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"docker.io"}`),
				Policy:     ownershipv1alpha1.PolicyNever,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "different type never matches",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"S3/v1","baseUrl":"s3.aws"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name: "baseUrl with embedded path matches split baseUrl+subPath form",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrEmbeddedPath,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "empty top-level policy with matching entry applies entry policy",
			cfg: makeCfg(t, "", &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "empty top-level policy with no matching entry → never",
			cfg: makeCfg(t, "", &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"docker.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name: "nil entries in repositories list are skipped",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever,
				nil,
				&ownershipv1alpha1.RepositoryPolicy{Repository: nil, Policy: ownershipv1alpha1.PolicyAddIfSupported},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
					Policy:     ownershipv1alpha1.PolicyAddIfSupported,
				},
			),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "unsupported entry policy → error after matching",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
				Policy:     "Bogus",
			}),
			target:  targetOCIGhcrMyOrg,
			wantErr: true,
		},
		{
			name:   "unknown target type with no entries skips identity extraction",
			cfg:    makeCfg(t, ownershipv1alpha1.PolicyAddIfSupported),
			target: targetUnknownType,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "unknown target type with entries fails identity extraction",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"UnregisteredRepository/v1"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target:  targetUnknownType,
			wantErr: true,
		},
		{
			name: "registered type alias on entry still matches canonical target type",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCI","baseUrl":"ghcr.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "invalid top-level policy is rejected even when an entry matches",
			cfg: makeCfg(t, "Bogus", &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target:  targetOCIGhcrMyOrg,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveOwnershipReferrerPolicy(repoSpec.Scheme, tc.cfg, tc.target)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOCIRepositoryIdentity(t *testing.T) {
	rawSpec := func(t *testing.T, jsonStr string) *runtime.Raw {
		t.Helper()
		raw := &runtime.Raw{}
		require.NoError(t, json.Unmarshal([]byte(jsonStr), raw))
		return raw
	}

	const ociIdentityType = "OCIRegistry"

	tests := []struct {
		name    string
		spec    *runtime.Raw
		want    runtime.Identity
		wantErr bool
	}{
		{
			name: "oci with type only → type-only identity",
			spec: rawSpec(t, `{"type":"OCIRepository/v1"}`),
			want: runtime.Identity{
				runtime.IdentityAttributeType: ociIdentityType,
			},
		},
		{
			name: "oci with baseUrl host only",
			spec: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
			want: runtime.Identity{
				runtime.IdentityAttributeType:     ociIdentityType,
				runtime.IdentityAttributeHostname: "ghcr.io",
			},
		},
		{
			name: "oci with baseUrl + subPath",
			spec: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
			want: runtime.Identity{
				runtime.IdentityAttributeType:     ociIdentityType,
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributePath:     "my-org/components",
			},
		},
		{
			name: "oci with path embedded in baseUrl",
			spec: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io/my-org/components"}`),
			want: runtime.Identity{
				runtime.IdentityAttributeType:     ociIdentityType,
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributePath:     "my-org/components",
			},
		},
		{
			name: "oci with embedded path AND explicit subPath joins both",
			spec: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io/my-org","subPath":"components"}`),
			want: runtime.Identity{
				runtime.IdentityAttributeType:     ociIdentityType,
				runtime.IdentityAttributeHostname: "ghcr.io",
				runtime.IdentityAttributePath:     "my-org/components",
			},
		},
		{
			name: "oci with scheme and port",
			spec: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"https://localhost:5000"}`),
			want: runtime.Identity{
				runtime.IdentityAttributeType:     ociIdentityType,
				runtime.IdentityAttributeHostname: "localhost",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "5000",
			},
		},
		{
			name:    "ctf is not supported → error",
			spec:    rawSpec(t, `{"type":"CommonTransportFormat/v1","filePath":"/tmp/ctf"}`),
			wantErr: true,
		},
		{
			name:    "unknown type → error",
			spec:    rawSpec(t, `{"type":"S3/v1","baseUrl":"s3.aws"}`),
			wantErr: true,
		},
		{
			name:    "malformed baseUrl → error",
			spec:    rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"https://example.com:abc"}`),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, err := convertToOCIRepository(repoSpec.Scheme, tc.spec)
			if err != nil {
				require.True(t, tc.wantErr, "unexpected conversion error: %v", err)
				return
			}
			got, err := ociRepositoryIdentity(repo)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
