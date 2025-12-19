// Package looseref provides a looser reference parser for OCI registry references.
//
// It extends ORAS's parser with two extra features:
// 1. References without registry components (e.g., "hello-world:v1")
// 2. Preserves the tag even when digest is present (e.g., "hello-world:v1@sha256:abc")
//
// Used by Open Component Model's references and maintains compatibility with standard OCI registry formats.
package looseref

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/opencontainers/go-digest"
	"oras.land/oras-go/v2/errdef"
	oras "oras.land/oras-go/v2/registry"
)

// tagRegexp checks the tag name.
// The docker and OCI spec have the same regular expression.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#pulling-manifests
var tagRegexp = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)

type LooseReference struct {
	Scheme string
	oras.Reference
	Tag string
}

// String implements `fmt.Stringer` and returns the reference string.
// It builds the string in the following order:
//  1. Registry and/or repository (e.g., "registry/repo" or "repo")
//  2. Tag prefixed with ":" if present (e.g., ":v1.0.0")
//  3. Digest prefixed with "@" if present (e.g., "@sha256:abc...")
func (r LooseReference) String() string {
	var result strings.Builder

	hasRegistry := r.Registry != ""
	hasRepository := r.Repository != ""
	hasBasePath := hasRegistry || hasRepository
	hasScheme := r.Scheme != ""

	if hasScheme && (hasRegistry || hasRepository) {
		result.WriteString(r.Scheme)
		result.WriteString("://")
	}

	switch {
	case hasRegistry && hasRepository:
		result.WriteString(r.Registry)
		result.WriteString("/")
		result.WriteString(r.Repository)
	case hasRegistry:
		result.WriteString(r.Registry)
	case hasRepository:
		result.WriteString(r.Repository)
	}

	// Add tag if present
	if r.Tag != "" {
		if hasBasePath {
			result.WriteString(":")
		}
		result.WriteString(r.Tag)
	}

	// Add digest if present
	if hasDigestAlgorithmPrefix(r.Reference.Reference) {
		result.WriteString("@")
		result.WriteString(r.Reference.Reference)
	}

	return result.String()
}

// ValidateTag validates the tag.
func (r LooseReference) ValidateTag() error {
	if !tagRegexp.MatchString(r.Tag) {
		return fmt.Errorf("%w: invalid tag %q", errdef.ErrInvalidReference, r.Reference)
	}
	return nil
}

func (r LooseReference) RegistryWithScheme() string {
	if r.Scheme != "" && r.Registry != "" {
		return r.Scheme + "://" + r.Registry
	}
	return r.Registry
}

// ReferenceOrTag returns the appropriate reference string following precedence:
//  1. Reference field if it's a valid digest
//  2. Tag field if Reference is a valid tag
//  3. Empty string otherwise
func (r LooseReference) ReferenceOrTag() string {
	switch {
	case r.ValidateReferenceAsDigest() == nil:
		return r.Reference.Reference
	case r.ValidateReferenceAsTag() == nil:
		return r.Tag
	}
	return ""
}

// hasDigestAlgorithmPrefix checks if the path starts with a known digest algorithm prefix.
// Uses the digest package to determine which algorithms are supported.
func hasDigestAlgorithmPrefix(path string) bool {
	idx := strings.Index(path, ":")
	if idx <= 0 {
		return false
	}
	algorithm := digest.Algorithm(path[:idx])
	return algorithm.Available()
}

// validateReference validates the components of a LooseReference.
// It validates the registry, repository, and reference (tag or digest) according to the OCI spec.
func validateReference(ref LooseReference) error {
	if ref.Registry != "" {
		if err := ref.ValidateRegistry(); err != nil {
			return err
		}
	}

	if ref.Repository != "" {
		if err := ref.ValidateRepository(); err != nil {
			return err
		}
	}

	if ref.Reference.Reference != "" {
		if err := ref.ValidateReference(); err != nil {
			return err
		}
	}

	if ref.Tag != "" {
		if err := ref.ValidateTag(); err != nil {
			return err
		}
	}

	return nil
}

// ParseReference parses a string (artifact) into an `artifact reference`.
// Corresponding cryptographic hash implementations are required to be imported
// as specified by https://pkg.go.dev/github.com/opencontainers/go-digest#readme-usage
// if the string contains a digest.
//
// Compared to ORAS ParseReference, this function extends the valid forms to:
//   - Registry is optional, and can contain a scheme prefix
//   - Tag is preserved when digest is present
//   - Bare digests are supported
//
// The reference string can take on the following forms:
//
//	ORAS Valid Forms:
//	<=== REPOSITORY ===> @ <=================== digest ===> |    - Valid Form A (digest preserved)
//	<=== REPOSITORY ===> : <!!! TAG !!!> @ <=== digest ===> |    - Valid Form B (tag dropped, digest preserved)
//	<=== REPOSITORY ===> : <=== TAG ======================> |    - Valid Form C (tag only)
//	<=== REPOSITORY ======================================> |    - Valid Form D (no tag or digest, repository only)
//	OCM Valid Forms:
//	<=== REPOSITORY ===> : <=== TAG ====> @ <=== digest ===>|    - OCM Valid Form (tag and digest preserved)
//	<=================== digest ==========================> |    - OCM Valid Form (digest only)
//
// Note that compared to OCI, in OCM Loose References can contain scheme prefixes (e.g., "oci://").
// This is mainly used to support switching connection behavior.
func ParseReference(artifact string) (LooseReference, error) {
	scheme, artifact := getScheme(artifact)

	if scheme != "" {
		if _, ok := allowedSchemes[scheme]; !ok {
			return LooseReference{}, fmt.Errorf("%w: invalid scheme %q", errdef.ErrInvalidReference, scheme)
		}
	}

	// Split the input artifact string into registry and path components.
	parts := strings.SplitN(artifact, "/", 2)
	var registry, path string

	if len(parts) == 1 {
		// Case: No registry specified, only repository (Valid Form E)
		registry = ""
		path = parts[0]
	} else {
		// Case: Registry and repository are specified
		registry = parts[0]
		path = parts[1]
	}

	var repository, reference, tag string

	if index := strings.Index(path, "@"); index != -1 {
		// Case: Digest is present; Valid Form A or B
		repository = path[:index]
		reference = path[index+1:]

		if jindex := strings.Index(repository, ":"); jindex != -1 {
			if strings.LastIndex(repository, ":") != jindex {
				return LooseReference{}, errdef.ErrInvalidReference
			}
			// Case: Tag is present along with digest; Valid Form B
			repository = repository[:jindex]
			tag = path[jindex+1 : index]
		}
	} else if index = strings.Index(path, ":"); index != -1 {
		if strings.LastIndex(path, ":") != index {
			return LooseReference{}, errdef.ErrInvalidReference
		}
		// Case: Only tag is present; Valid Form C
		// Special case: treat digest algorithm prefixes (e.g., "sha256:abc", "sha512:xyz") without registry as tag-only reference
		if len(parts) == 1 && hasDigestAlgorithmPrefix(path) {
			reference = path
		} else {
			repository = path[:index]
			tag = path[index+1:]
			reference = tag
		}
	} else {
		// Case: No tag or digest; Valid Form D or E
		repository = path
	}

	ref := LooseReference{
		Scheme: scheme,
		Reference: oras.Reference{
			Registry:   registry,
			Repository: repository,
			Reference:  reference,
		},
		Tag: tag,
	}

	if err := validateReference(ref); err != nil {
		return LooseReference{}, err
	}

	return ref, nil
}

var allowedSchemes = map[string]struct{}{
	"oci":   {},
	"http":  {},
	"https": {},
}

// getScheme extracts a leading scheme of the form "scheme://path".
func getScheme(raw string) (scheme, rest string) {
	// Strong form: scheme://path
	if i := strings.Index(raw, "://"); i > 0 && i < len(raw)-3 {
		return raw[:i], raw[i+3:]
	}
	return "", raw
}
