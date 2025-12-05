package graph

import (
	"fmt"
	"regexp"
)

var (
	// ErrNamingConvention is the base error message for naming convention violations
	ErrNamingConvention = "naming convention violation"
)

// validateTransformationGraphDefinitionNamingConventions validates the naming conventions of
// the given resource graph definition.
func validateTransformationGraphDefinitionNamingConventions(transformations map[string]*Transformation) error {
	err := validateResourceIDs(transformations)
	if err != nil {
		return fmt.Errorf("%s: %w", ErrNamingConvention, err)
	}
	return nil
}

var (
	// lowerCamelCaseRegex
	lowerCamelCaseRegex = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

	// reservedKeyWords is a list of reserved words in kro.
	reservedKeyWords = []string{
		"apiVersion",
		"context",
		"dependency",
		"dependencies",
		"externalRef",
		"externalReference",
		"externalRefs",
		"externalReferences",
		"graph",
		"instance",
		"item",
		"items",
		"kind",
		"metadata",
		"namespace",
		"object",
		"resource",
		"resources",
		"root",
		"runtime",
		"schema",
		"self",
		"spec",
		"status",
		"this",
		"variables",
		"vars",
		"version",
	}
)

// validateResource performs basic validation on a given transfergraphdefinition.
// It checks that there are no duplicate resource ids and that the
// resource ids are conformant to the OCM naming convention.
//
// The OCM naming convention is as follows:
// - The id should start with a lowercase letter.
// - The id should only contain alphanumeric characters.
// - Does not contain any special characters, underscores, or hyphens.
func validateResourceIDs(transformations map[string]*Transformation) error {
	seen := make(map[string]struct{})
	for _, transformation := range transformations {
		meta := transformation.TransformationMeta
		if isOCMReservedWord(meta.ID) {
			return fmt.Errorf("id %s is a reserved keyword in OCM", meta.ID)
		}

		if !isValidResourceID(meta.ID) {
			return fmt.Errorf("id %s is not a valid OCM transformations id: must be lower camelCase", meta.ID)
		}

		if _, ok := seen[meta.ID]; ok {
			return fmt.Errorf("found duplicate transformations IDs %s", meta.ID)
		}
		seen[meta.ID] = struct{}{}
	}
	return nil
}

// isOCMReservedWord checks if the given word is a reserved word in OCM.
func isOCMReservedWord(word string) bool {
	for _, w := range reservedKeyWords {
		if w == word {
			return true
		}
	}
	return false
}

// isValidResourceID checks if the given id is a valid OCM resource id (loawercase)
func isValidResourceID(id string) bool {
	return lowerCamelCaseRegex.MatchString(id)
}
