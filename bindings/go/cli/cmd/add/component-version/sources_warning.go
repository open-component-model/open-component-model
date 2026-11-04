package componentversion

import (
	"fmt"
	"io"
	"strings"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
)

// componentsWithSources lists, in constructor order, the components of spec
// that declare sources.
func componentsWithSources(spec *constructorruntime.ComponentConstructor) []string {
	var names []string
	for _, component := range spec.Components {
		if len(component.Sources) > 0 {
			names = append(names, component.Name+":"+component.Version)
		}
	}
	return names
}

// renderSourcesWarning writes a prominent warning to w when components declare
// sources, and nothing otherwise. Sources carry no digest, so neither the
// component version digest nor a signature binds their content: whoever
// controls the source location can swap it unnoticed. Resources are digested
// and therefore the safe way to ship content.
func renderSourcesWarning(w io.Writer, components []string) {
	if len(components) == 0 {
		return
	}
	banner := strings.Repeat("!", 78)
	lines := []string{
		banner,
		"WARNING: SOURCES ARE DEPRECATED AND UNSAFE",
		"The following component versions declare sources:",
	}
	for _, c := range components {
		lines = append(lines, "  - "+c)
	}
	lines = append(lines,
		"Sources have NO DIGEST: their content is not covered by the component",
		"version digest or any signature and can be replaced without detection.",
		"Declare this content as resources instead, see the migration guide:",
		"https://ocm.software/docs/changelog/migrate-from-sources/",
		banner,
	)
	_, _ = fmt.Fprintln(w)
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, l)
	}
}
