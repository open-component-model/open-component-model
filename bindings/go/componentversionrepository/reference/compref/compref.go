// Package compref provides functionality to parse component references used in OCM (Open Component Model).
package compref

import (
	"cmp"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	resolverv1 "ocm.software/open-component-model/bindings/go/component
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
// +k8s:deepcopy-gen=true
type Ref struct {
	// Type represents the repository type (e.g., "oci", "ctf")
	Type string

	// Repository is the location of the component repository
	Repository runtime.Typed

	// FallbackRepositories is an optional list of fallback repositories to attempt if the primary repository fails.
	// If only fallbacks are specified, the first fallback repository is used as Repository.
	FallbackRepositories []runtime.Typed

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

type ParseOptions struct {
	// Aliases is a map of aliases to raw repository specifications.
	// The keys are the alias names, and the values are the raw repository specifications.
	// This allows for custom repository definitions that can be referenced by alias.
	// An example alias might look like:
	// "ghcr" => { "type": "OCIRepository/v1", "baseUrl": "ghcr.io/organization/repository" }
	//
	// When parsing a component reference that starts with an alias, the parser will look up the alias
	// in this map and use the corresponding raw repository specification to create the Ref.
	Aliases map[string]*runtime.Raw `json:"aliases,omitempty"`

	// FallbackResolvers is a list of resolvers that can be used to resolve component references.
	FallbackResolvers []*resolverv1.Resolver `json:"resolvers,omitempty"`
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
//   - <repositoryAlias>//[<DefaultPrefix>]/<component id>[:<version>] - repository aliases for the ocm configuration
//   - <component id>[:<version>] - component name without repository, using the configured fallback resolvers (must have at least one resolver configured)
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
func Parse(input string, opts *ParseOptions) (*Ref, error) {
	originalInput := input

	// Try to handle alias first
	if ref, err := refFromAlias(input, opts); err != nil {
		return nil, err
	} else if ref != nil {
		return ref, nil
	}

	ref := &Ref{}

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
		// If no prefix is found, try to handle as a component-only reference with fallback resolvers
		if fallbackRef, err := handleWithoutRepoSpec(input, opts); err == nil {
			// Copy version and digest from the original ref
			fallbackRef.Version = ref.Version
			fallbackRef.Digest = ref.Digest
			return fallbackRef, nil
		}
		return nil, fmt.Errorf("no valid descriptor prefix found in %q (expected one of: %v)", originalInput, ValidPrefixes)
	}

	// Step 5: Validate component name
	if !componentRegex.MatchString(ref.Component) {
		return nil, fmt.Errorf("invalid component name %q in %q, must match %q", ref.Component, originalInput, ComponentRegex)
	}

	// Step 6: Resolve type if not explicitly given
	if ref.Type == "" {
		t, err := GuessType(input)
		if err != nil {
			return nil, fmt.Errorf("failed to detect repository type from %q: %w", input, err)
		}
		Base.Debug("ocm had to guess your repository type", "type", t, "input", input)
		ref.Type = t
	}

	// Step 7: Build repository object
	typed, err := createRepository(ref.Type, input)
	if err != nil {
		return nil, err
	}
	ref.Repository = typed

	ref.FallbackRepositories, _ = getFallbackRepositorySpecs(opts, ref.Component)

	return ref, nil
}

// GuessType tries to guess the repository type ("ctf" or "oci")
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
//   - If it looks like a domain (contains dots like ".com", ".io", etc.), assume OCI
//   - If it contains a colon (e.g., "localhost:5000"), assume OCI
//   - Otherwise fallback to CTF
func GuessType(repository string) (string, error) {
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

	// Contains domain-looking part (e.g., github.com, ghcr.io) → assume OCI
	if looksLikeDomain(cleaned) {
		return runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(), nil
	}

	// Default fallback: assume CTF
	return runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(), nil
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

// refFromAlias processes a component reference that starts with an alias.
// It returns the parsed Ref if successful, or nil if the input doesn't match any alias.
func refFromAlias(input string, opts *ParseOptions) (*Ref, error) {
	if opts == nil || opts.Aliases == nil {
		return nil, nil
	}

	// Try to find a matching alias
	for alias, raw := range opts.Aliases {
		if !strings.HasPrefix(input, alias+"//") {
			continue
		}

		// Found a matching alias, use the raw repository spec
		repoSpec := input[len(alias)+2:] // Skip the alias and "//"
		if repoSpec == "" {
			return nil, fmt.Errorf("missing component after alias %q in %q", alias, input)
		}

		// Create a new repository object from the raw spec
		typed, err := RepositoryScheme.NewObject(raw.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to create repository from alias %q: %w", alias, err)
		}

		// Convert the raw spec to the typed object
		if err := RepositoryScheme.Convert(raw, typed); err != nil {
			return nil, fmt.Errorf("failed to convert repository spec from alias %q: %w", alias, err)
		}

		ref := &Ref{
			Type:       raw.Type.String(),
			Repository: typed,
		}

		// Check if the first part is a valid prefix
		parts := strings.SplitN(repoSpec, "/", 2)
		if len(parts) == 2 {
			// If the first part is a valid prefix, use it
			if parts[0] == DefaultPrefix {
				ref.Prefix = DefaultPrefix
				// Get the component name from the rest
				ref.Component = parts[1]
			} else {
				// If not a valid prefix, treat the entire string as the component name
				ref.Component = repoSpec
			}
		}

		// Extract version and digest if present
		if idx := strings.LastIndex(ref.Component, ":"); idx != -1 {
			versionPart := ref.Component[idx+1:]
			ref.Component = ref.Component[:idx]

			// Check for digest
			if digestIdx := strings.LastIndex(versionPart, "@"); digestIdx != -1 {
				ref.Digest = versionPart[digestIdx+1:]
				versionPart = versionPart[:digestIdx]
			}

			if versionPart != "" {
				if !versionRegex.MatchString(versionPart) {
					return nil, fmt.Errorf("invalid semantic version %q in %q, must match %q", versionPart, input, VersionRegex)
				}
				ref.Version = versionPart
			}
		}

		// Validate component name
		if !componentRegex.MatchString(ref.Component) {
			return nil, fmt.Errorf("invalid component name %q in %q, must match %q", ref.Component, input, ComponentRegex)
		}

		return ref, nil
	}

	return nil, nil
}

// createRepository creates a repository object based on the type and input string.
func createRepository(typ string, input string) (runtime.Typed, error) {
	rtyp, err := runtime.TypeFromString(typ)
	if err != nil {
		return nil, fmt.Errorf("unknown type %q: %w", typ, err)
	}

	typed, err := RepositoryScheme.NewObject(rtyp)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository of type %q: %w", typ, err)
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
		return nil, fmt.Errorf("unsupported repository type: %q", typ)
	}

	return typed, nil
}

// handleWithoutRepoSpec processes fallback repositories from the given resolvers.
// It returns a Ref with the first fallback repository as the primary repository and the rest as fallbacks.
// If no valid fallback repositories are found, it returns an error.
func handleWithoutRepoSpec(input string, opts *ParseOptions) (*Ref, error) {
	if opts == nil || len(opts.FallbackResolvers) == 0 {
		return nil, fmt.Errorf("no fallback resolvers available")
	}

	ref := &Ref{
		Component: input,
		Prefix:    DefaultPrefix,
	}

	fallbacks, err := getFallbackRepositorySpecs(opts, ref.Component)
	if err != nil {
		return nil, err
	}

	if len(fallbacks) == 0 {
		return nil, fmt.Errorf("no valid fallback repositories found in resolvers")
	}

	ref.FallbackRepositories = fallbacks

	// Use the first fallback repository as the primary repository
	ref.Repository = ref.FallbackRepositories[0]
	ref.FallbackRepositories = ref.FallbackRepositories[1:]

	// Validate component name
	if !componentRegex.MatchString(ref.Component) {
		return nil, fmt.Errorf("invalid component name %q, must match %q", ref.Component, ComponentRegex)
	}

	return ref, nil
}

func getFallbackRepositorySpecs(opts *ParseOptions, component string) ([]runtime.Typed, error) {
	if opts == nil || len(opts.FallbackResolvers) == 0 {
		return nil, nil
	}

	// Sort resolvers by priority
	resolvers := make([]runtime.Typed, 0, len(opts.FallbackResolvers))

	actual := slices.Clone(opts.FallbackResolvers)

	slices.SortStableFunc(actual, func(a, b *resolverv1.Resolver) int {
		return cmp.Compare(b.Priority, a.Priority)
	})

	// Create fallback repositories from resolvers
	for _, resolver := range actual {
		if resolver.Repository == nil {
			continue
		}

		typed, err := RepositoryScheme.NewObject(resolver.Repository.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to create repository from fallback resolver: %w", err)
		}

		if err := RepositoryScheme.Convert(resolver.Repository, typed); err != nil {
			return nil, fmt.Errorf("failed to convert repository spec from fallback resolver: %w", err)
		}

		if resolver.Prefix != "" {
			switch typed.(type) {
			case *ociv1.Repository:
				// For OCI repositories, ensure the base URL is fitting the prefix
				if !strings.HasPrefix(component, resolver.Prefix) {
					continue
				}
			case *ctfv1.Repository:
				// For CTF repositories, ensure the path is fitting the prefix
				if !strings.HasPrefix(component, resolver.Prefix) {
					continue
				}
			default:
				slog.Warn("prefix attribute for an unknown repository type is ignored")
			}
		}

		resolvers = append(resolvers, typed)
	}

	return resolvers, nil
}
