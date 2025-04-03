package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

type indexedIdentity struct {
	meta  ElementMeta
	index int
}

// Validate checks for duplicate identities across resources, sources, and references in a component descriptor.
// An identity is considered duplicate if it has the same name, version, and extra identity attributes.
// The function returns an error with detailed information about any duplicates found, including:
// - The type of element (resource/source/reference)
// - The exact identity that was duplicated
// - The number of times it appears
// - The indices in the original descriptor where the duplicates appear
func Validate(desc *Descriptor) error {
	// Create maps to track identities by their canonical hash
	resourceIdentities := make(map[uint64][]indexedIdentity)
	sourceIdentities := make(map[uint64][]indexedIdentity)
	referenceIdentities := make(map[uint64][]indexedIdentity)

	// Check resources for duplicate identities
	for i, resource := range desc.Component.Resources {
		id := resource.ElementMeta.ToIdentity().CanonicalHashV1()
		resourceIdentities[id] = append(resourceIdentities[id], indexedIdentity{resource.ElementMeta, i})
	}

	// Check sources for duplicate identities
	for i, source := range desc.Component.Sources {
		id := source.ElementMeta.ToIdentity().CanonicalHashV1()
		sourceIdentities[id] = append(sourceIdentities[id], indexedIdentity{source.ElementMeta, i})
	}

	// Check references for duplicate identities
	for i, ref := range desc.Component.References {
		id := ref.ElementMeta.ToIdentity().CanonicalHashV1()
		referenceIdentities[id] = append(referenceIdentities[id], indexedIdentity{ref.ElementMeta, i})
	}

	// Check for duplicates and create detailed error messages
	var errors []string

	if duplicates := findDuplicates(resourceIdentities, "resource"); len(duplicates) > 0 {
		errors = append(errors, fmt.Sprintf("duplicate resource identities found: %s", duplicates))
	}

	if duplicates := findDuplicates(sourceIdentities, "source"); len(duplicates) > 0 {
		errors = append(errors, fmt.Sprintf("duplicate source identities found: %s", duplicates))
	}

	if duplicates := findDuplicates(referenceIdentities, "reference"); len(duplicates) > 0 {
		errors = append(errors, fmt.Sprintf("duplicate reference identities found: %s", duplicates))
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation failed:\n- %s", strings.Join(errors, "\n- "))
	}

	return nil
}

// findDuplicates returns a description of duplicate elements
func findDuplicates(identities map[uint64][]indexedIdentity, elementType string) string {
	var duplicates []string
	for _, elements := range identities {
		if len(elements) > 1 {
			// Use the first element's identity for the error message
			identity := elements[0].meta.ToIdentity()

			// Create a list of indices for the duplicates
			indices := make([]string, len(elements))
			for i, elem := range elements {
				indices[i] = strconv.Itoa(elem.index)
			}

			duplicates = append(duplicates, fmt.Sprintf("identity '%v' appears %d times at %s indices [%s]",
				identity,
				len(elements),
				elementType,
				strings.Join(indices, ", ")))
		}
	}
	if len(duplicates) > 0 {
		return strings.Join(duplicates, "; ")
	}
	return ""
}
