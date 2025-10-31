package looseref

import (
	_ "crypto/sha256"
	_ "crypto/sha512"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry"
)

const ValidDigest = "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
const InvalidDigest = "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde"
const ValidDigest512 = "sha512:ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"
const InvalidDigest512 = "sha512:ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49"

// For a definition of what a "valid form [ABCD]" means, see reference.go.
func TestParseReferenceGoodies(t *testing.T) {
	tests := []struct {
		name         string
		image        string
		wantTemplate LooseReference
	}{
		{
			name:  "digest reference (valid form A)",
			image: fmt.Sprintf("hello-world@%s", ValidDigest),
			wantTemplate: LooseReference{
				Reference: registry.Reference{
					Repository: "hello-world",
					Reference:  ValidDigest,
				},
			},
		},
		{
			name:  "tag with digest (valid form B)",
			image: fmt.Sprintf("hello-world:v2@%s", ValidDigest),
			wantTemplate: LooseReference{
				Reference: registry.Reference{
					Repository: "hello-world",
					Reference:  ValidDigest,
				},
				Tag: "v2",
			},
		},
		{
			name:  "empty tag with digest (valid form B)",
			image: fmt.Sprintf("hello-world:@%s", ValidDigest),
			wantTemplate: LooseReference{
				Reference: registry.Reference{
					Repository: "hello-world",
					Reference:  ValidDigest,
				},
			},
		},
		{
			name:  "tag reference (valid form C)",
			image: "hello-world:v1",
			wantTemplate: LooseReference{
				Reference: registry.Reference{
					Repository: "hello-world",
					Reference:  "v1",
				},
				Tag: "v1",
			},
		},
		{
			name:  "basic reference (valid form D)",
			image: "hello-world",
			wantTemplate: LooseReference{
				Reference: registry.Reference{
					Repository: "hello-world",
				},
			},
		},
	}

	registries := []string{
		"localhost",
		"registry.example.com",
		"localhost:5000",
		"127.0.0.1:5000",
		"[::1]:5000",
		"",
	}

	for _, tt := range tests {
		want := tt.wantTemplate
		for _, registry := range registries {
			want.Registry = registry
			t.Run(tt.name, func(t *testing.T) {
				ref := fmt.Sprintf("%s/%s", registry, tt.image)
				if registry == "" {
					ref = tt.image
				}
				got, err := ParseReference(ref)
				require.NoErrorf(t, err, "ParseReference() encountered unexpected error: %v", err)
				require.Equalf(t, want, got, "ParseReference() = %v, want %v", got, tt.wantTemplate)
			})
		}
	}
}

func TestLooseParseReference(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		wantTemplate LooseReference
	}{
		{
			name: "CTF style reference",
			ref:  "component-descriptors/test-component:v1.0.0",
			wantTemplate: LooseReference{
				Reference: registry.Reference{
					Registry:   "component-descriptors",
					Repository: "test-component",
					Reference:  "v1.0.0",
				},
				Tag: "v1.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReference(tt.ref)
			require.NoErrorf(t, err, "ParseReference() encountered unexpected error: %v", err)
			require.Equal(t, tt.wantTemplate, got)
		})
	}
}

func TestParseReferenceUglies(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want registry.Reference
	}{
		{
			name: "invalid repo name",
			raw:  "localhost/UPPERCASE/test",
		},
		{
			name: "invalid port",
			raw:  "localhost:v1/hello-world",
		},
		{
			name: "invalid digest",
			raw:  fmt.Sprintf("registry.example.com/foobar@%s", InvalidDigest),
		},
		{
			name: "invalid sha512 digest",
			raw:  fmt.Sprintf("registry.example.com/hello-world@%s", InvalidDigest512),
		},
		{
			name: "invalid digest prefix: colon instead of the at sign",
			raw:  fmt.Sprintf("registry.example.com/hello-world:foobar:%s", ValidDigest),
		},
		{
			name: "invalid digest prefix: double at sign",
			raw:  fmt.Sprintf("registry.example.com/hello-world@@%s", ValidDigest),
		},
		{
			name: "invalid digest prefix: space",
			raw:  fmt.Sprintf("registry.example.com/hello-world @%s", ValidDigest),
		},
		{
			name: "repository with sha256-like tag containing colon is invalid",
			raw:  "myrepo:sha256:abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseReference(tt.raw)
			require.Empty(t, ref)
			require.Errorf(t, err, "ParseReference() expected an error, but got reg=%v,repo=%v,ref=%v", ref.Registry, ref.Repository, ref.Reference)
		})
	}
}

func TestLooseReferenceString(t *testing.T) {
	tests := []struct {
		name     string
		ref      LooseReference
		expected string
	}{
		{
			name: "registry only",
			ref: LooseReference{
				Reference: registry.Reference{
					Registry: "localhost:5000",
				},
			},
			expected: "localhost:5000",
		},
		{
			name: "repository only",
			ref: LooseReference{
				Reference: registry.Reference{
					Repository: "hello-world",
				},
			},
			expected: "hello-world",
		},
		{
			name: "repository and tag",
			ref: LooseReference{
				Reference: registry.Reference{
					Repository: "hello-world",
					Reference:  "latest",
				},
				Tag: "latest",
			},
			expected: "hello-world:latest",
		},
		{
			name: "registry and repository",
			ref: LooseReference{
				Reference: registry.Reference{
					Registry:   "localhost:5000",
					Repository: "hello-world",
				},
			},
			expected: "localhost:5000/hello-world",
		},
		{
			name: "with tag",
			ref: LooseReference{
				Reference: registry.Reference{
					Registry:   "localhost:5000",
					Repository: "hello-world",
				},
				Tag: "v1",
			},
			expected: "localhost:5000/hello-world:v1",
		},
		{
			name: "with digest",
			ref: LooseReference{
				Reference: registry.Reference{
					Registry:   "localhost:5000",
					Repository: "hello-world",
					Reference:  ValidDigest,
				},
			},
			expected: "localhost:5000/hello-world@" + ValidDigest,
		},
		{
			name: "with tag and digest",
			ref: LooseReference{
				Reference: registry.Reference{
					Registry:   "localhost:5000",
					Repository: "hello-world",
					Reference:  ValidDigest,
				},
				Tag: "v1",
			},
			expected: "localhost:5000/hello-world:v1@" + ValidDigest,
		},
		{
			name: "empty reference",
			ref: LooseReference{
				Reference: registry.Reference{},
			},
			expected: "",
		},
		{
			name: "tag and digest only",
			ref: LooseReference{
				Reference: registry.Reference{
					Reference: ValidDigest,
				},
				Tag: "v1",
			},
			expected: "v1@" + ValidDigest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ref.String()
			require.Equal(t, tt.expected, got, "String() = %v, want %v", got, tt.expected)
		})
	}
}
