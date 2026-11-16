// Package looseref provides a looser reference parser for OCI registry references.
//
// It extends ORAS's parser with two extra features:
// 1. References without registry components (e.g., "hello-world:v1")
// 2. Bare digests without registry or repository (e.g., "sha256:abc")
//
// Used by Open Component Model's references and maintains compatibility with standard OCI registry formats.
package looseref

import (
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/oras-project/oras-go/v3/errdef"
	"github.com/oras-project/oras-go/v3/registry/remote/properties"
)

type LooseReference struct {
	Scheme string
	properties.Reference
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
	if r.Digest != "" {
		result.WriteString("@")
		result.WriteString(r.Digest)
	}

	return result.String()
}

func (r LooseReference) RegistryWithScheme() string {
	if r.Scheme != "" && r.Registry != "" {
		return r.Scheme + "://" + r.Registry
	}
	return r.Registry
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
			if ref.Repository != strings.ToLower(ref.Repository) {
				return fmt.Errorf("%w: repository names must be lowercase", err)
			}
			return err
		}
	}

	if ref.Digest != "" {
		if err := ref.ValidateDigest(); err != nil {
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
// Compared to ORAS properties.NewReference, this function extends the valid forms to:
//   - Registry is optional, and can contain a scheme prefix
//   - Bare digests are supported
//   - Registry is not lower-cased
//
// The reference string can take on the following forms:
//
//	ORAS Valid Forms:
//	<=== REPOSITORY ===> @ <=================== digest ===> |    - Valid Form A (digest only)
//	<=== REPOSITORY ===> : <=== TAG ====> @ <=== digest ===>|    - Valid Form B (tag and digest)
//	<=== REPOSITORY ===> : <=== TAG ======================> |    - Valid Form C (tag only)
//	<=== REPOSITORY ======================================> |    - Valid Form D (no tag or digest, repository only)
//	OCM Valid Forms:
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

	var repository, dgst, tag string

	if index := strings.Index(path, "@"); index != -1 {
		// Case: Digest is present; Valid Form A or B
		repository = path[:index]
		dgst = path[index+1:]

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
		// Special case: digest algorithm prefixes (e.g., "sha256:abc", "sha512:xyz") without registry are a bare digest
		if len(parts) == 1 && hasDigestAlgorithmPrefix(path) {
			dgst = path
		} else {
			repository = path[:index]
			tag = path[index+1:]
		}
	} else {
		// Case: No tag or digest; Valid Form D or E
		repository = path
	}

	ref := LooseReference{
		Scheme: scheme,
		Reference: properties.Reference{
			Registry:   registry,
			Repository: repository,
			Tag:        tag,
			Digest:     dgst,
		},
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
