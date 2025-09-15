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
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
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
	// DigestRegex is the regular expression used to validate digests as part of a component reference.
	DigestRegex = `[A-Za-z][A-Za-z0-9]*(?:[-_+.][A-Za-z][A-Za-z0-9]*)*[:][[:xdigit:]]{32,}`
)

var (
	componentRegex = regexp.MustCompile(ComponentRegex)
	versionRegex   = regexp.MustCompile(VersionRegex)
	digestRegex    = regexp.MustCompile(DigestRegex)
)

// DefaultPrefix is the default prefix used for component descriptors.
const DefaultPrefix = "component-descriptors"

// ctfArchiveExtensions is the list of archive file extensions that should be treated as CTF
var ctfArchiveExtensions = [...]string{".tar.gz", ".tgz", ".tar"}

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
//	[<type>::]<repository>/[<valid-prefix>]/<component>[:<version>][@<digest>]
//
// For valid prefixes, see ValidPrefixes.
// For valid components, see ComponentRegex.
// For valid versions, see VersionRegex.
// For valid digests, see DigestRegex.
type Ref struct {
	// Type represents the repository type (e.g., "oci", "ctf")
	Type string

	// Repository is the location of the component repository
	Repository runtime.Typed

	// Prefix is an optional path element that helps structure components within a repository
	// It can only be one of ValidPrefixes.
	Prefix string

	// Component is the name of the component.
	// Validated as per ComponentRegex.
	Component string

	// Version is the semantic version of the component. It can be specified without Digest,
	// in which case it is a "soft" version pinning in that the content behind the version
	// can change without the specification becoming invalid.
	// Validated as per VersionRegex.
	Version string

	// Digest is an optional content-addressable identifier for a pinned component version (e.g., sha256:abcd...)
	// if present, it indicates a specific version of the component MUST be present with this digest.
	// Thus, the Digest is more authoritative than the Version.
	// Validates as per DigestRegex.
	Digest string
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
	if ref.Digest != "" {
		sb.WriteString("@" + ref.Digest)
	}
	return sb.String()
}

// Parse parses an input string into a Ref.
// Accepted inputs are of the forms
//
//   - [ctf::][<file path>/[<DefaultPrefix>]/<component id>[:<version>][@<digest>]
//   - [oci::][<registry>/<repository>/[<DefaultPrefix>]/<component id>[:<version>][@<digest>]
//   - localhost[:<port>]/[<DefaultPrefix>]/<component id>[:<version>] - localhost special cases
//
// Not accepted cases that were valid in old OCM:
//
//   - [type::][<repositorySpecJSON>/[<DefaultPrefix>]/<component id>[:<version>] - arbitrary repository definitions
//
// All non-supported special cases are currently under review of being accepted forms.
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

	// Step 2: Extract optional digest (e.g., @sha256:...)
	var digestPart string
	if idx := strings.LastIndex(input, "@"); idx != -1 && !strings.Contains(input[idx:], "/") {
		digestPart = input[idx+1:]
		input = input[:idx]

		if !digestRegex.MatchString(digestPart) {
			return nil, fmt.Errorf("invalid digest %q in %q, must match %q", digestPart, originalInput, DigestRegex)
		}
		ref.Digest = digestPart
	}

	// Step 3: Extract optional version (e.g., :1.2.3)
	var versionPart string
	if idx := strings.LastIndex(input, ":"); idx != -1 && !strings.Contains(input[idx:], "/") {
		versionPart = input[idx+1:]
		input = input[:idx]

		if !versionRegex.MatchString(versionPart) {
			return nil, fmt.Errorf("invalid semantic version %q in %q, must match %q", versionPart, originalInput, VersionRegex)
		}
		ref.Version = versionPart
	}

	// Step 4: Find prefix
	foundPrefix := false
	for _, prefix := range ValidPrefixes {
		token := "/" + prefix + "/"
		if idx := strings.LastIndex(input, token); idx != -1 {
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

	// Step 5: Validate component name
	if !componentRegex.MatchString(ref.Component) {
		return nil, fmt.Errorf("invalid component name %q in %q, must match %q", ref.Component, originalInput, ComponentRegex)
	}

	// Step 6: Build repository object using ParseRepository
	var repositoryRef string
	if ref.Type != "" {
		repositoryRef = ref.Type + "::" + input
	} else {
		repositoryRef = input
	}

	repository, err := ParseRepository(repositoryRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository: %w", err)
	}

	ref.Repository = repository

	// Extract the type from the parsed repository for consistency
	if ref.Type == "" {
		switch repository.(type) {
		case *ociv1.Repository:
			ref.Type = runtime.NewVersionedType(ociv1.Type, ociv1.Version).String()
		case *ctfv1.Repository:
			ref.Type = runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String()
		}
	}

	return ref, nil
}

// ParseRepository parses a repository reference string and returns a typed repository object.
// It accepts repository strings in the format:
//   - [<type>::]<repository-ref>
//
// Where type can be "ctf" or "oci", and repository reference is the actual repository location.
// If no type is specified, it will be guessed using heuristics.
func ParseRepository(repoRef string) (runtime.Typed, error) {
	originalInput := repoRef
	input := repoRef

	// Extract optional type
	var repoType string
	if idx := strings.Index(input, "::"); idx != -1 {
		repoType = input[:idx]
		input = input[idx+2:]
	}

	// Resolve type if isn't explicitly given
	if repoType == "" {
		t, err := guessType(input)
		if err != nil {
			return nil, fmt.Errorf("failed to detect repository type from %q: %w", input, err)
		}
		Base.Debug("ocm had to guess your repository type", "type", t, "input", input)
		repoType = t
	}

	// Build repository object
	rtyp, err := runtime.TypeFromString(repoType)
	if err != nil {
		return nil, fmt.Errorf("unknown type %q in %q: %w", repoType, originalInput, err)
	}

	typed, err := RepositoryScheme.NewObject(rtyp)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository of type %q: %w", repoType, err)
	}

	switch t := typed.(type) {
	case *ociv1.Repository:
		uri, err := url.Parse(input)
		if err != nil {
			return nil, fmt.Errorf("failed to parse repository URI %q: %w", input, err)
		}
		t.BaseUrl = uri.String()
	case *ctfv1.Repository:
		t.Path = input
	default:
		return nil, fmt.Errorf("unsupported repository type: %q", repoType)
	}

	return typed, nil
}

// guessType tries to guess the repository type ("ctf" or "oci")
// from an untyped repository specification string.
//
// You may ask yourself why this is needed.
// The reason is that there are some repository strings that are indistinguishable from being either
// a CTF or OCI repository. For example,
// "github.com/organization/repository" could be an OCI repository without a Scheme,
// but it could also be a file path to a CTF in the subfolders "github.com", "organization" and "repository".
//
// It uses a practical set of heuristics:
//   - If it has a URL scheme ("file://"), assume CTF
//   - If it's an absolute filesystem path, assume CTF
//   - If it contains a colon (e.g., "localhost:5000"), assume OCI
//   - If it looks like an archive file (tar.gz, tgz or tar), assume CTF
//   - If it looks like a domain (contains dots like ".com", ".io", etc.), assume OCI
//   - Otherwise fallback to CTF
func guessType(repository string) (string, error) {
	// Try parsing as URL first
	if u, err := url.Parse(repository); err == nil {
		if u.Scheme == "file" {
			return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
		}
		if u.Scheme != "" {
			// Any other scheme (e.g., https) implies OCI
			return runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(), nil
		}
	}

	cleaned := filepath.Clean(repository)

	// Absolute filesystem path → assume CTF
	if filepath.IsAbs(cleaned) {
		return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
	}

	// Contains colon (e.g., localhost:5000), or is localhost without port → assume OCI
	if strings.Contains(cleaned, ":") || cleaned == "localhost" {
		return runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(), nil
	}

	// Check if it looks like an archive file → assume CTF
	if looksLikeArchive(cleaned) {
		return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
	}

	// Contains domain-looking part (e.g., github.com, ghcr.io) → assume OCI
	if looksLikeDomain(cleaned) {
		return runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(), nil
	}

	// Default fallback: assume CTF
	return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
}

// looksLikeArchive checks if the string ends with tar.gz or tgz archive file extensions.
// This helps identify repository strings that point to archive files, which should be treated as CTF.
func looksLikeArchive(s string) bool {
	s = strings.ToLower(s)
	for _, ext := range ctfArchiveExtensions {
		if strings.HasSuffix(s, ext) {
			return true
		}
	}
	return false
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
