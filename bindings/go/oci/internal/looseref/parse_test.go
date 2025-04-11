package looseref

import (
	_ "crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	. "oras.land/oras-go/v2/registry"
)

const ValidDigest = "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
const InvalidDigest = "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde"

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
				Reference: Reference{
					Repository: "hello-world",
					Reference:  ValidDigest,
				},
			},
		},
		{
			name:  "tag with digest (valid form B)",
			image: fmt.Sprintf("hello-world:v2@%s", ValidDigest),
			wantTemplate: LooseReference{
				Reference: Reference{
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
				Reference: Reference{
					Repository: "hello-world",
					Reference:  ValidDigest,
				},
			},
		},
		{
			name:  "tag reference (valid form C)",
			image: "hello-world:v1",
			wantTemplate: LooseReference{
				Reference: Reference{
					Repository: "hello-world",
				},
				Tag: "v1",
			},
		},
		{
			name:  "basic reference (valid form D)",
			image: "hello-world",
			wantTemplate: LooseReference{
				Reference: Reference{
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
				got, err := LooseParseReference(ref)
				if err != nil {
					t.Errorf("ParseReference() encountered unexpected error: %v", err)
					return
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("ParseReference() = %v, want %v", got, tt.wantTemplate)
				}
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
				Reference: Reference{
					Registry:   "component-descriptors",
					Repository: "test-component",
				},
				Tag: "v1.0.0",
			},
		},
	}

	for _, tt := range tests {
		want := tt.wantTemplate
		t.Run(tt.name, func(t *testing.T) {
			got, err := LooseParseReference(tt.ref)
			if err != nil {
				t.Errorf("ParseReference() encountered unexpected error: %v", err)
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ParseReference() = %v, want %v", got, tt.wantTemplate)
			}
		})
	}
}

func TestParseReferenceUglies(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Reference
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ref, err := LooseParseReference(tt.raw); err == nil {
				t.Errorf("ParseReference() expected an error, but got reg=%v,repo=%v,ref=%v", ref.Registry, ref.Repository, ref.Reference)
				return
			}
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
				Reference: Reference{
					Registry: "localhost:5000",
				},
			},
			expected: "localhost:5000",
		},
		{
			name: "repository only",
			ref: LooseReference{
				Reference: Reference{
					Repository: "hello-world",
				},
			},
			expected: "hello-world",
		},
		{
			name: "registry and repository",
			ref: LooseReference{
				Reference: Reference{
					Registry:   "localhost:5000",
					Repository: "hello-world",
				},
			},
			expected: "localhost:5000/hello-world",
		},
		{
			name: "with tag",
			ref: LooseReference{
				Reference: Reference{
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
				Reference: Reference{
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
				Reference: Reference{
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
				Reference: Reference{},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ref.String()
			if got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
