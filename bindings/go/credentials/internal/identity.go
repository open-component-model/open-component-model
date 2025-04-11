package internal

import (
	"maps"
	"slices"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func IdentityToString(id runtime.Identity) string {
	var builder strings.Builder
	for i, key := range slices.Sorted(maps.Keys(id)) {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(key + "=" + id[key])
	}
	return builder.String()
}
