package looseref

import (
	_ "crypto/sha256"
	_ "crypto/sha512"
	"fmt"
	"reflect"
	"testing"

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
				Reference: registry.Reference{
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
			got, err := ParseReference(tt.ref)
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

func TestParseReferenceSha256AsTag(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		want      LooseReference
		wantError bool
	}{
		{
			name: "valid sha256 hash as tag-only reference",
			ref:  "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			want: LooseReference{
				Reference: registry.Reference{},
				Tag:       "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
			},
			wantError: false,
		},
		{
			name: "sha256 prefix with repository should parse normally",
			ref:  "myrepo:sha256abc",
			want: LooseReference{
				Reference: registry.Reference{
					Repository: "myrepo",
				},
				Tag: "sha256abc",
			},
			wantError: false,
		},
		{
			name:      "repository with sha256-like tag containing colon is invalid",
			ref:       "myrepo:sha256:abc",
			wantError: true,
		},
		{
			name: "valid full sha512 hash as tag-only reference",
			ref:  "sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
			want: LooseReference{
				Reference: registry.Reference{},
				Tag:       "sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReference(tt.ref)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseReference() expected error but got none, result: %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseReference() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseReference() = %#v, want %#v", got, tt.want)
			}
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
			if ref, err := ParseReference(tt.raw); err == nil {
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
