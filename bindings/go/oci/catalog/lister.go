package catalog

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	ocipath "ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
	repo "ocm.software/open-component-model/bindings/go/repository"
)

// ErrFnNil reports that ListComponents was called without a callback function.
var ErrFnNil = errors.New("expected a valid callback function, but got nil")

// ComponentLister implements repository.ComponentLister for remote OCI
// registries. It gets raw repository names from a RepositoryCataloger and maps
// them to OCM component names. The mapping keeps repositories that contain the
// component-descriptors/ segment, strips the optional subPath prefix and the
// component-descriptors/ prefix, and returns the remainder as the component
// name.
type ComponentLister struct {
	cataloger RepositoryCataloger
	subPath   string
}

var _ repo.ComponentLister = (*ComponentLister)(nil)

// NewComponentLister creates a ComponentLister for the given cataloger and
// subPath. subPath is the OCM repository prefix and may be empty.
func NewComponentLister(c RepositoryCataloger, subPath string) *ComponentLister {
	return &ComponentLister{
		cataloger: c,
		subPath:   subPath,
	}
}

// ListComponents lists OCM component names found in the registry. It calls fn
// once per catalog page with the component names of that page, lexically
// sorted and deduplicated. If the cataloger supports pagination, last is the
// last component name of the previous page. ListComponents maps last back to
// its repository name so the cataloger continues after it.
func (l *ComponentLister) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	if fn == nil {
		return ErrFnNil
	}

	return l.cataloger.Repositories(ctx, repoCursorFor(l.subPath, last), func(repos []string) error {
		names := componentNamesFromRepos(repos, l.subPath)
		if len(names) == 0 {
			return nil
		}
		return fn(names)
	})
}

// repoCursorFor maps a component name cursor back to its repository name so it
// can be used as the last parameter of a catalog page request. An empty
// component cursor stays empty so listing starts from the top.
func repoCursorFor(subPath, last string) string {
	if last == "" {
		return ""
	}
	return componentDescriptorPrefix(subPath) + last
}

// componentNamesFromRepos maps raw repository names to OCM component names. It
// keeps repositories that carry the component-descriptors/ segment under the
// optional subPath prefix, strips both prefixes, and returns the remaining
// component names deduplicated and lexically sorted.
func componentNamesFromRepos(repos []string, subPath string) []string {
	prefix := componentDescriptorPrefix(subPath)

	accumulated := make(map[string]struct{})
	for _, r := range repos {
		if !strings.HasPrefix(r, prefix) {
			continue
		}
		accumulated[strings.TrimPrefix(r, prefix)] = struct{}{}
	}

	names := slices.Collect(maps.Keys(accumulated))
	slices.Sort(names)
	return names
}

// firstPathSegment returns the first path segment of an OCM subPath, or an
// empty string when the subPath carries no segment. Vendor catalogers that
// scope a listing to one namespace or owner use it to read that first segment
// from the subPath.
func firstPathSegment(subPath string) string {
	trimmed := strings.Trim(subPath, "/")
	if trimmed == "" {
		return ""
	}
	if i := strings.Index(trimmed, "/"); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

// componentDescriptorPrefix returns the repository prefix that OCM component
// descriptors live under for the given subPath, for example
// "component-descriptors/" or "my/sub/component-descriptors/".
func componentDescriptorPrefix(subPath string) string {
	descriptors := ocipath.DefaultComponentDescriptorPath + "/"
	if sub := strings.Trim(subPath, "/"); sub != "" {
		return sub + "/" + descriptors
	}
	return descriptors
}
