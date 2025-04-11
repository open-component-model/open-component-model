package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptorRuntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const jsonData = `
{
  "meta": {
    "schemaVersion": "v2"
  },
  "component": {
    "name": "github.com/weaveworks/weave-gitops",
    "version": "v1.0.0",
    "provider": "weaveworks",
    "labels": [
      {
        "name": "link-to-documentation",
        "value": "https://github.com/weaveworks/weave-gitops"
      }
    ],
    "repositoryContexts": [
      {
        "baseUrl": "ghcr.io",
        "componentNameMapping": "urlPath",
        "subPath": "phoban01/ocm",
        "type": "OCIRegistry"
      }
    ],
    "resources": [
      {
        "name": "image",
        "relation": "external",
        "type": "ociImage",
        "version": "v0.14.1",
        "access": {
          "type": "ociArtifact",
          "imageReference": "ghcr.io/weaveworks/wego-app:v0.14.1"
        },
        "digest": {
          "hashAlgorithm": "SHA-256",
          "normalisationAlgorithm": "ociArtifactDigest/v1",
          "value": "efa2b9980ca2de65dc5a0c8cc05638b1a4b4ce8f6972dc08d0e805e5563ba5bb"
        }
      }
    ],
    "sources": [
      {
        "name": "weave-gitops",
        "type": "git",
        "version": "v0.14.1",
        "access": {
          "commit": "727513969553bfcc603e1c0ae1a75d79e4132b58",
          "ref": "refs/tags/v0.14.1",
          "repoUrl": "github.com/weaveworks/weave-gitops",
          "type": "gitHub"
        }
      }
    ],
    "componentReferences": [
      {
        "name": "prometheus",
        "version": "v1.0.0",
        "componentName": "cncf.io/prometheus",
        "digest": {
          "hashAlgorithm": "SHA-256",
          "normalisationAlgorithm": "jsonNormalisation/v1",
          "value": "04eb20b6fd942860325caf7f4415d1acf287a1aabd9e4827719328ba25d6f801"
        }
      }
    ]
  },
  "signatures": [
    {
      "name": "ww-dev",
      "digest": {
        "hashAlgorithm": "SHA-256",
        "normalisationAlgorithm": "jsonNormalisation/v1",
        "value": "4faff7822616305ecd09284d7c3e74a64f2269dcc524a9cdf0db4b592b8cee6a"
      },
      "signature": {
        "algorithm": "RSASSA-PSS",
        "mediaType": "application/vnd.ocm.signature.rsa",
        "value": "26468587671bdbd2166cf5f69829f090c10768511b15e804294fcb26e552654316c8f4851ed396f279ec99335e5f4b11cb043feb97f1f9a42115f4fda2d31ae8b481b7303b9a913d3a4b92d446fbee9ed487c93b09e513f3f68355040ec08454675e1f407422062abbd2681f70dd5488ad29020b30cfa7e001455c550458da96166bc3243c8426977d73352aface5323fb2b5a374e9c31b272a59c160b85631231c9fc2f23c032401b80fef937029a39111cee34470c61ae86cd4942553466411a5a116159fdcc10e50fe9360c5184028e72d1fe9c7315f26e15d7b4849f62d197501b8cc6b6f1b1391ecc2fc2fc0c1290d2554594505b25fa8f9bfb28c8df24"
      }
    }
  ]
}`

func TestDescriptorString(t *testing.T) {
	d := descriptorRuntime.Descriptor{
		Meta: descriptorRuntime.Meta{Version: "v1"},
		Component: descriptorRuntime.Component{
			ComponentMeta: descriptorRuntime.ComponentMeta{
				ObjectMeta: descriptorRuntime.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	expected := "test-component:1.0.0 (schema version v1)"
	if d.String() != expected {
		t.Errorf("expected %s, got %s", expected, d.String())
	}
}

func TestComponentString(t *testing.T) {
	c := descriptorRuntime.Component{
		ComponentMeta: descriptorRuntime.ComponentMeta{
			ObjectMeta: descriptorRuntime.ObjectMeta{
				Name:    "test-component",
				Version: "1.0.0",
			},
		},
	}

	expected := "test-component:1.0.0"
	if c.String() != expected {
		t.Errorf("expected %s, got %s", expected, c.String())
	}
}

func TestConvertFromAndToV2(t *testing.T) {
	var v2Descriptor v2.Descriptor
	err := json.Unmarshal([]byte(jsonData), &v2Descriptor)
	require.NoError(t, err)

	descriptor, err := descriptorRuntime.ConvertFromV2(&v2Descriptor)
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	convertedV2Descriptor, err := descriptorRuntime.ConvertToV2(scheme, descriptor)
	require.NoError(t, err)

	assert.Equal(t, v2Descriptor, *convertedV2Descriptor)
}

func TestConvertToV2(t *testing.T) {
	var v2Descriptor v2.Descriptor
	err := json.Unmarshal([]byte(jsonData), &v2Descriptor)
	require.NoError(t, err)

	descriptor, err := descriptorRuntime.ConvertFromV2(&v2Descriptor)
	require.NoError(t, err)

	scheme := runtime.NewScheme()

	convertedV2Descriptor, err := descriptorRuntime.ConvertToV2(scheme, descriptor)
	require.NoError(t, err)

	assert.Equal(t, v2Descriptor, *convertedV2Descriptor)
	assert.NotEmpty(t, convertedV2Descriptor.Component.Resources[0].Name)
	assert.NotEmpty(t, convertedV2Descriptor.Component.Resources[0].Access.Data)
}

func TestConvertFromV2Provider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     runtime.Identity
		wantErr  bool
	}{
		{
			name:     "simple provider name",
			provider: "test-provider",
			want: runtime.Identity{
				v2.IdentityAttributeName: "test-provider",
			},
			wantErr: false,
		},
		{
			name:     "json provider",
			provider: `{"name": "test-provider", "type": "org"}`,
			want: runtime.Identity{
				"name": "test-provider",
				"type": "org",
			},
			wantErr: false,
		},
		{
			name:     "invalid json",
			provider: `{invalid json}`,
			want:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := descriptorRuntime.ConvertFromV2Provider(tt.provider)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV2Labels(t *testing.T) {
	tests := []struct {
		name   string
		labels []v2.Label
		want   []descriptorRuntime.Label
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   nil,
		},
		{
			name:   "empty labels",
			labels: []v2.Label{},
			want:   []descriptorRuntime.Label{},
		},
		{
			name: "with labels",
			labels: []v2.Label{
				{Name: "test", Value: "value", Signing: true},
			},
			want: []descriptorRuntime.Label{
				{Name: "test", Value: "value", Signing: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertFromV2Labels(tt.labels)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV2Digest(t *testing.T) {
	tests := []struct {
		name   string
		digest *v2.Digest
		want   *descriptorRuntime.Digest
	}{
		{
			name:   "nil digest",
			digest: nil,
			want:   nil,
		},
		{
			name: "valid digest",
			digest: &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "jsonNormalisation/v1",
				Value:                  "test-value",
			},
			want: &descriptorRuntime.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "jsonNormalisation/v1",
				Value:                  "test-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertFromV2Digest(tt.digest)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV2SourceRefs(t *testing.T) {
	tests := []struct {
		name string
		refs []v2.SourceRef
		want []descriptorRuntime.SourceRef
	}{
		{
			name: "nil refs",
			refs: nil,
			want: nil,
		},
		{
			name: "empty refs",
			refs: []v2.SourceRef{},
			want: []descriptorRuntime.SourceRef{},
		},
		{
			name: "with refs",
			refs: []v2.SourceRef{
				{
					IdentitySelector: map[string]string{"name": "test"},
					Labels:           []v2.Label{{Name: "test", Value: "value"}},
				},
			},
			want: []descriptorRuntime.SourceRef{
				{
					IdentitySelector: map[string]string{"name": "test"},
					Labels:           []descriptorRuntime.Label{{Name: "test", Value: "value"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertFromV2SourceRefs(tt.refs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV2Sources(t *testing.T) {
	tests := []struct {
		name    string
		sources []v2.Source
		want    []descriptorRuntime.Source
	}{
		{
			name:    "nil sources",
			sources: nil,
			want:    nil,
		},
		{
			name:    "empty sources",
			sources: []v2.Source{},
			want:    []descriptorRuntime.Source{},
		},
		{
			name: "with sources",
			sources: []v2.Source{
				{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "test",
							Version: "1.0.0",
							Labels:  []v2.Label{{Name: "test", Value: "value"}},
						},
					},
					Type: "test-type",
				},
			},
			want: []descriptorRuntime.Source{
				{
					ElementMeta: descriptorRuntime.ElementMeta{
						ObjectMeta: descriptorRuntime.ObjectMeta{
							Name:    "test",
							Version: "1.0.0",
							Labels:  []descriptorRuntime.Label{{Name: "test", Value: "value"}},
						},
					},
					Type: "test-type",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertFromV2Sources(tt.sources)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV2References(t *testing.T) {
	tests := []struct {
		name       string
		references []v2.Reference
		want       []descriptorRuntime.Reference
	}{
		{
			name:       "nil references",
			references: nil,
			want:       nil,
		},
		{
			name:       "empty references",
			references: []v2.Reference{},
			want:       []descriptorRuntime.Reference{},
		},
		{
			name: "with references",
			references: []v2.Reference{
				{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "test",
							Version: "1.0.0",
							Labels:  []v2.Label{{Name: "test", Value: "value"}},
						},
					},
					Digest: v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
				},
			},
			want: []descriptorRuntime.Reference{
				{
					ElementMeta: descriptorRuntime.ElementMeta{
						ObjectMeta: descriptorRuntime.ObjectMeta{
							Name:    "test",
							Version: "1.0.0",
							Labels:  []descriptorRuntime.Label{{Name: "test", Value: "value"}},
						},
					},
					Digest: descriptorRuntime.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertFromV2References(tt.references)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV2Signatures(t *testing.T) {
	tests := []struct {
		name       string
		signatures []v2.Signature
		want       []descriptorRuntime.Signature
	}{
		{
			name:       "nil signatures",
			signatures: nil,
			want:       nil,
		},
		{
			name:       "empty signatures",
			signatures: []v2.Signature{},
			want:       []descriptorRuntime.Signature{},
		},
		{
			name: "with signatures",
			signatures: []v2.Signature{
				{
					Name: "test",
					Digest: v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
					Signature: v2.SignatureInfo{
						Algorithm: "test-algo",
						Value:     "test-value",
						MediaType: "test-media",
						Issuer:    "test-issuer",
					},
				},
			},
			want: []descriptorRuntime.Signature{
				{
					Name: "test",
					Digest: descriptorRuntime.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
					Signature: descriptorRuntime.SignatureInfo{
						Algorithm: "test-algo",
						Value:     "test-value",
						MediaType: "test-media",
						Issuer:    "test-issuer",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertFromV2Signatures(tt.signatures)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV2Provider(t *testing.T) {
	tests := []struct {
		name     string
		provider runtime.Identity
		want     string
		wantErr  bool
	}{
		{
			name:     "nil provider",
			provider: nil,
			want:     "",
			wantErr:  false,
		},
		{
			name: "simple provider",
			provider: runtime.Identity{
				v2.IdentityAttributeName: "test-provider",
			},
			want:    "test-provider",
			wantErr: false,
		},
		{
			name: "provider without name",
			provider: runtime.Identity{
				"other": "value",
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := descriptorRuntime.ConvertToV2Provider(tt.provider)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV2Labels(t *testing.T) {
	tests := []struct {
		name   string
		labels []descriptorRuntime.Label
		want   []v2.Label
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   nil,
		},
		{
			name:   "empty labels",
			labels: []descriptorRuntime.Label{},
			want:   []v2.Label{},
		},
		{
			name: "with labels",
			labels: []descriptorRuntime.Label{
				{Name: "test", Value: "value", Signing: true},
			},
			want: []v2.Label{
				{Name: "test", Value: "value", Signing: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertToV2Labels(tt.labels)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV2Digest(t *testing.T) {
	tests := []struct {
		name   string
		digest *descriptorRuntime.Digest
		want   *v2.Digest
	}{
		{
			name:   "nil digest",
			digest: nil,
			want:   nil,
		},
		{
			name: "valid digest",
			digest: &descriptorRuntime.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "jsonNormalisation/v1",
				Value:                  "test-value",
			},
			want: &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "jsonNormalisation/v1",
				Value:                  "test-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertToV2Digest(tt.digest)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV2SourceRefs(t *testing.T) {
	tests := []struct {
		name string
		refs []descriptorRuntime.SourceRef
		want []v2.SourceRef
	}{
		{
			name: "nil refs",
			refs: nil,
			want: nil,
		},
		{
			name: "empty refs",
			refs: []descriptorRuntime.SourceRef{},
			want: []v2.SourceRef{},
		},
		{
			name: "with refs",
			refs: []descriptorRuntime.SourceRef{
				{
					IdentitySelector: map[string]string{"name": "test"},
					Labels:           []descriptorRuntime.Label{{Name: "test", Value: "value"}},
				},
			},
			want: []v2.SourceRef{
				{
					IdentitySelector: map[string]string{"name": "test"},
					Labels:           []v2.Label{{Name: "test", Value: "value"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertToV2SourceRefs(tt.refs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV2References(t *testing.T) {
	tests := []struct {
		name       string
		references []descriptorRuntime.Reference
		want       []v2.Reference
	}{
		{
			name:       "nil references",
			references: nil,
			want:       nil,
		},
		{
			name:       "empty references",
			references: []descriptorRuntime.Reference{},
			want:       []v2.Reference{},
		},
		{
			name: "with references",
			references: []descriptorRuntime.Reference{
				{
					ElementMeta: descriptorRuntime.ElementMeta{
						ObjectMeta: descriptorRuntime.ObjectMeta{
							Name:    "test",
							Version: "1.0.0",
							Labels:  []descriptorRuntime.Label{{Name: "test", Value: "value"}},
						},
					},
					Digest: descriptorRuntime.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
				},
			},
			want: []v2.Reference{
				{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "test",
							Version: "1.0.0",
							Labels:  []v2.Label{{Name: "test", Value: "value"}},
						},
					},
					Digest: v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertToV2References(tt.references)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV2Signatures(t *testing.T) {
	tests := []struct {
		name       string
		signatures []descriptorRuntime.Signature
		want       []v2.Signature
	}{
		{
			name:       "nil signatures",
			signatures: nil,
			want:       nil,
		},
		{
			name:       "empty signatures",
			signatures: []descriptorRuntime.Signature{},
			want:       []v2.Signature{},
		},
		{
			name: "with signatures",
			signatures: []descriptorRuntime.Signature{
				{
					Name: "test",
					Digest: descriptorRuntime.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
					Signature: descriptorRuntime.SignatureInfo{
						Algorithm: "test-algo",
						Value:     "test-value",
						MediaType: "test-media",
						Issuer:    "test-issuer",
					},
				},
			},
			want: []v2.Signature{
				{
					Name: "test",
					Digest: v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "jsonNormalisation/v1",
						Value:                  "test-value",
					},
					Signature: v2.SignatureInfo{
						Algorithm: "test-algo",
						Value:     "test-value",
						MediaType: "test-media",
						Issuer:    "test-issuer",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := descriptorRuntime.ConvertToV2Signatures(tt.signatures)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertFromV2LocalBlob(t *testing.T) {
	scheme := runtime.NewScheme()
	tests := []struct {
		name    string
		blob    *v2.LocalBlob
		want    *descriptorRuntime.LocalBlob
		wantErr bool
	}{
		{
			name: "nil blob",
			blob: nil,
			want: nil,
		},
		{
			name: "basic blob",
			blob: &v2.LocalBlob{
				Type: runtime.Type{
					Name:    "test",
					Version: "v1",
				},
				LocalReference: "test-ref",
				MediaType:      "test/media",
				ReferenceName:  "test-name",
			},
			want: &descriptorRuntime.LocalBlob{
				Type: runtime.Type{
					Name:    "test",
					Version: "v1",
				},
				LocalReference: "test-ref",
				MediaType:      "test/media",
				ReferenceName:  "test-name",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := descriptorRuntime.ConvertFromV2LocalBlob(scheme, tt.blob)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToV2LocalBlob(t *testing.T) {
	scheme := runtime.NewScheme()
	tests := []struct {
		name    string
		blob    *descriptorRuntime.LocalBlob
		want    *v2.LocalBlob
		wantErr bool
	}{
		{
			name: "nil blob",
			blob: nil,
			want: nil,
		},
		{
			name: "basic blob",
			blob: &descriptorRuntime.LocalBlob{
				Type: runtime.Type{
					Name:    "test",
					Version: "v1",
				},
				LocalReference: "test-ref",
				MediaType:      "test/media",
				ReferenceName:  "test-name",
			},
			want: &v2.LocalBlob{
				Type: runtime.Type{
					Name:    "test",
					Version: "v1",
				},
				LocalReference: "test-ref",
				MediaType:      "test/media",
				ReferenceName:  "test-name",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := descriptorRuntime.ConvertToV2LocalBlob(scheme, tt.blob)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
