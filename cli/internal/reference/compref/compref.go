// Package compref provides functionality to parse component references used in OCM (Open Component Model).
package compref

import (
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Base = slog.With(slog.String("realm", "compref"))

const (
	// ComponentRegex is the regular expression used to validate component names.
	// For details see https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/02-elements-toplevel.md#component-identity
	ComponentRegex = `^[a-z][-a-z0-9]*([.][a-z][-a-z0-9]*)*[.][a-z]{2,}(/[a-z][-a-z0-9_]*([.][a-z][-a-z0-9_]*)*)+$`
	// VersionRegex is the regular expression used to validate semantic versioning in "loose" format.
	// It allows for optional "v" prefix, and supports pre-release and build metadata.
	// The regex is based on the semantic versioning specification (https://semver.org/spec/v2.0.0.html).
	VersionRegex = `^[v]?(0|[1-9]\d*)(?:\.(0|[1-9]\d*))?(?:\.(0|[1-9]\d*))?(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`
)

var (
	componentRegex = regexp.MustCompile(ComponentRegex)
	versionRegex   = regexp.MustCompile(VersionRegex)
)

// DefaultPrefix is the default prefix used for component descriptors.
const DefaultPrefix = "component-descriptors"

// ValidPrefixes is the list of valid prefixes for structured component references
var ValidPrefixes = []string{
	DefaultPrefix, // for component descriptors this is the default prefix
	"",            // empty prefix is also valid, indicating no specific prefix
}

var RepositoryScheme = runtime.NewScheme(runtime.WithAllowUnknown())

func init() {
	repository.MustAddToScheme(RepositoryScheme)
	repository.MustAddLegacyToScheme(RepositoryScheme)
}

// Ref represents the parsed structure of an OCM component reference.
// A component reference is a string that uniquely identifies a component in a repository.
//
// The format of a component reference is:
//
//	[<type>::]<repository>/[<valid-prefix>]/<component>[:<version>]
//
// For valid prefixes, see ValidPrefixes.
// For valid versions, see VersionRegex.
type Ref struct {
	// Type represents the repository type (e.g., "oci", "ctf")
	Type string

	// Repository is the location of the component repository
	Repository runtime.Typed

	// Prefix is an optional path element that helps structure components within a repository
	// It can only be one of ValidPrefixes.
	Prefix string

	// Component is the name of the component
	Component string

	// Version is the semantic version of the component
	Version string
}

func (ref *Ref) String() string {
	var sb strings.Builder
	if ref.Type != "" {
		sb.WriteString(ref.Type + "::")
	}
	sb.WriteString(fmt.Sprintf("%v", ref.Repository) + "/" + ref.Prefix + "/" + ref.Component)
	if ref.Version != "" {
		sb.WriteString(":" + ref.Version)
	}
	return sb.String()
}

// Parse parses an input string into a Ref.
// Accepted inputs are of the forms
//
//   - [ctf::][<file path>/[<DefaultPrefix>]/<component id>[:<version>]
//   - [oci::][<registry>/<repository>/[<DefaultPrefix>]/<component id>[:<version>]
//
// Not accepted cases that were valid in old OCM:
//
//   - [type::][<repositorySpecJSON>/[<DefaultPrefix>]/<component id>[:<version>] - arbitrary repository definitions
//   - [oci::][<registry>/<repository>/[<DefaultPrefix>]/<component id>[:<version>][@<digest>] - pinned component versions
//   - <repositoryAlias>//[<DefaultPrefix>]/<component id>[:<version>] - repository aliases for the ocm configuration
//   - localhost[:<port>]/[<DefaultPrefix>]/<component id>[:<version>] - localhost special cases
//
// All non-supported special cases are currently under review of being accepted forms.
// TODO(jakobmoellerdev): Add support for component version pinning via digest.
//
// This code roughly resembles
// https://github.com/open-component-model/ocm/blob/2ea69c7ecca1e8be7e9d9f94dfdcac6090f1c69d/api/oci/ref_test.go
// in a much smaller scope and size and will grow over time.
func Parse(input string) (*Ref, error) {
	ref := &Ref{}
	originalInput := input

	// Step 1: Extract optional type
	if idx := strings.Index(input, "::"); idx != -1 {
		ref.Type = input[:idx]
		input = input[idx+2:]
	}

	// Step 2: Extract optional version
	var versionPart string
	if idx := strings.LastIndex(input, ":"); idx != -1 && !strings.Contains(input[idx:], "/") {
		versionPart = input[idx+1:]
		input = input[:idx]

		if !versionRegex.MatchString(versionPart) {
			return nil, fmt.Errorf("invalid semantic version %q in %q, must match %q", versionPart, originalInput, VersionRegex)
		}
		ref.Version = versionPart
	}

	// Step 3: Find prefix
	foundPrefix := false
	for _, prefix := range ValidPrefixes {
		token := "/" + prefix + "/"
		if idx := strings.Index(input, token); idx != -1 {
			repoSpec := input[:idx]
			rest := input[idx+len(token):]

			if rest == "" {
				return nil, fmt.Errorf("missing component after prefix in %q", originalInput)
			}

			ref.Prefix = prefix
			ref.Component = rest
			input = repoSpec
			foundPrefix = true
			break
		}
	}

	if !foundPrefix {
		return nil, fmt.Errorf("no valid descriptor prefix found in %q (expected one of: %v)", originalInput, ValidPrefixes)
	}

	// Step 4: Validate component name
	if !componentRegex.MatchString(ref.Component) {
		return nil, fmt.Errorf("invalid component name %q in %q, must match %q", ref.Component, originalInput, ComponentRegex)
	}

	// Step 5: Resolve type if not explicitly given
	if ref.Type == "" {
		t, err := GuessType(input)
		if err != nil {
			return nil, fmt.Errorf("failed to detect repository type from %q: %w", input, err)
		}
		Base.Debug("ocm had to guess your repository type", "type", t, "input", input)
		ref.Type = t
	}

	// Step 6: Build repository object
	rtyp, err := runtime.TypeFromString(ref.Type)
	if err != nil {
		return nil, fmt.Errorf("unknown type %q in %q: %w", ref.Type, originalInput, err)
	}

	typed, err := RepositoryScheme.NewObject(rtyp)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository of type %q: %w", ref.Type, err)
	}

	switch t := typed.(type) {
	case *v1.OCIRepository:
		t.BaseUrl = input
	case *v1.CTFRepository:
		t.Path = input
	default:
		return nil, fmt.Errorf("unsupported repository type: %q", ref.Type)
	}

	ref.Repository = typed

	return ref, nil
}

// GuessType tries to guess the repository type ("ctf" or "oci")
// from an untyped repository specification string.
//
// It uses a practical set of heuristics:
//   - If it has a URL scheme ("file://"), assume CTF
//   - If it's an absolute filesystem path, assume CTF
//   - If it looks like a domain (contains dots like ".com", ".io", etc.), assume OCI
//   - If it contains a colon (e.g., "localhost:5000"), assume OCI
//   - Otherwise fallback to CTF
func GuessType(repository string) (string, error) {
	// Try parsing as URL first
	if u, err := url.Parse(repository); err == nil {
		if u.Scheme == "file" {
			return runtime.NewVersionedType(v1.TypeCTF, v1.Version).String(), nil
		}
		if u.Scheme != "" {
			// Any other scheme (e.g., https) implies OCI
			return runtime.NewVersionedType(v1.Type, v1.Version).String(), nil
		}
	}

	cleaned := filepath.Clean(repository)

	// Absolute filesystem path → assume CTF
	if filepath.IsAbs(cleaned) {
		return runtime.NewVersionedType(v1.TypeCTF, v1.Version).String(), nil
	}

	// Contains colon (e.g., localhost:5000), or is localhost without port → assume OCI
	if strings.Contains(cleaned, ":") || cleaned == "localhost" {
		return runtime.NewVersionedType(v1.Type, v1.Version).String(), nil
	}

	// Contains domain-looking part (e.g., github.com, ghcr.io) → assume OCI
	if looksLikeDomain(cleaned) {
		return runtime.NewVersionedType(v1.Type, v1.Version).String(), nil
	}

	// Default fallback: assume CTF
	return runtime.NewVersionedType(v1.TypeCTF, v1.Version).String(), nil
}

// looksLikeDomain checks if the string contains a dot with non-numeric parts (heuristic).
// this makes it so that a path like "my.path" is always considered a domain, and if it should
// be interpreted as path, it needs to be passed explicitly
func looksLikeDomain(s string) bool {
	if strings.Contains(s, ".") {
		for _, part := range strings.Split(s, ".") {
			if part == "" {
				continue
			}
			for _, r := range part {
				if unicode.IsLetter(r) {
					return true
				}
			}
		}
	}
	return false
}
