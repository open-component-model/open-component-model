package sbom

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// extensions names the file a document of a given predicate type is written to.
var extensions = map[string]string{
	repository.PredicateTypeSPDX:      ".spdx.json",
	repository.PredicateTypeCycloneDX: ".cdx.json",
}

// Directory names the directory the SBOMs of a resource are written into when no output
// location was given. An identity renders as "name=image,architecture=amd64", which a
// shell needs quoting for and reads as an assignment, so only its values are used, the
// resource name leading: "image-amd64".
func Directory(identity runtime.Identity) string {
	keys := slices.Sorted(maps.Keys(identity))
	if index := slices.Index(keys, descriptor.IdentityAttributeName); index > 0 {
		keys = slices.Insert(slices.Delete(keys, index, index+1), 0, descriptor.IdentityAttributeName)
	}

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := safe(identity[key]); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "sboms"
	}

	return strings.Join(parts, "-")
}

// Write writes every discovered SBOM into dir, one file per document, byte for byte as
// published, creating dir if it does not exist. It returns the paths written, in the
// order the documents were given.
func Write(sboms []repository.SBOM, dir string) ([]string, error) {
	if len(sboms) == 0 {
		return nil, fmt.Errorf("no sbom documents to write")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating sbom output directory %q failed: %w", dir, err)
	}

	written := make([]string, 0, len(sboms))
	taken := make(map[string]struct{}, len(sboms))
	for _, discovered := range sboms {
		path := filepath.Join(dir, fileName(discovered, taken))
		if filepath.Dir(path) != filepath.Clean(dir) {
			return nil, fmt.Errorf("refusing to write sbom %q outside of %q", discovered, dir)
		}
		if err := os.WriteFile(path, discovered.Data, 0o600); err != nil {
			return nil, fmt.Errorf("writing sbom %q failed: %w", discovered, err)
		}
		written = append(written, path)
	}

	return written, nil
}

// fileName derives the name of one document's file, suffixing it when an earlier
// document already took the name.
func fileName(discovered repository.SBOM, taken map[string]struct{}) string {
	stem := safe(discovered.Name)
	if stem == "" {
		// A document that names itself is rare outside BuildKit, the id is a digest.
		stem = safe(discovered.ID)
	}
	if stem == "" {
		stem = "sbom"
	}
	if platform := safe(discovered.Platform.String()); platform != "" {
		stem += "_" + platform
	}

	extension, known := extensions[discovered.PredicateType]
	if !known {
		extension = ".json"
	}

	candidate := stem + extension
	for i := 2; ; i++ {
		if _, exists := taken[candidate]; !exists {
			taken[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", stem, i, extension)
	}
}

// safe folds everything that is not plainly a file name character to an underscore.
func safe(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
