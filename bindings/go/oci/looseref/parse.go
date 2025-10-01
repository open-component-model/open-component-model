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

	"oras.land/oras-go/v2/errdef"
	oras "oras.land/oras-go/v2/registry"
)

// tagRegexp checks the tag name.
// The docker and OCI spec have the same regular expression.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#pulling-manifests
var tagRegexp = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)

type LooseReference struct {
	oras.Reference
	Tag string
}

// String implements `fmt.Stringer` and returns the reference string.
// The resulted string is meaningful only if the reference is valid.
func (r LooseReference) String() string {
	var ref string

	switch {
	case r.Repository == "" && r.Registry != "":
		ref = r.Registry
	case r.Repository != "" && r.Registry == "":
		ref = r.Repository
	default:
		ref = r.Registry + "/" + r.Repository
	}

	if ref == "/" {
		return ""
	}

	if r.Reference.Reference == "" && r.Tag == "" {
		return ref
	}

	if r.Tag != "" {
		ref += ":" + r.Tag
	}

	if d, err := r.Digest(); err == nil {
		return ref + "@" + d.String()
	}

	return ref
}

// ValidateReferenceAsTag validates the reference as a tag.
func (r LooseReference) ValidateReferenceAsTag() error {
	if !tagRegexp.MatchString(r.Tag) {
		return fmt.Errorf("%w: invalid tag %q", errdef.ErrInvalidReference, r.Reference)
	}
	return nil
}

// ParseReference parses a string (artifact) into an `artifact reference`.
// Corresponding cryptographic hash implementations are required to be imported
// as specified by https://pkg.go.dev/github.com/opencontainers/go-digest#readme-usage
// if the string contains a digest.
// Compared to `ParseReference` from ORAS, this function is more lenient and allows for
// no registry (Valid Form E). This is useful for passing references to the `oras` Interfaces
// that do not have registries set. It also exposes the Tag (the tag in oras ParseReference gets
// removed when a digest is present)
func ParseReference(artifact string) (LooseReference, error) {
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
		repository = path[:index]
		tag = path[index+1:]
	} else {
		// Case: No tag or digest; Valid Form D or E
		repository = path
	}

	ref := LooseReference{
		Reference: oras.Reference{
			Registry:   registry,
			Repository: repository,
			Reference:  reference,
		},
		Tag: tag,
	}

	if len(registry) > 0 {
		// Validate the registry component
		if err := ref.ValidateRegistry(); err != nil {
			return LooseReference{}, err
		}
	}

	// Validate the repository component
	if err := ref.ValidateRepository(); err != nil {
		return LooseReference{}, err
	}

	// If a reference (tag or digest) is present, validate it
	if len(ref.Reference.Reference) > 0 {
		validator := ref.ValidateReferenceAsDigest
		if len(tag) > 0 && len(reference) == 0 {
			validator = ref.ValidateReferenceAsTag
		}
		if err := validator(); err != nil {
			return LooseReference{}, err
		}
	}

	return ref, nil
}
