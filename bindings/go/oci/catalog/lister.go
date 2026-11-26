package catalog

import (
	"context"
	"errors"
	"log/slog"
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

// errBlockComplete stops the catalog scan once it has passed the block of
// matching repositories. It is swallowed by ListComponents.
var errBlockComplete = errors.New("component descriptor block complete")

// ListComponents lists OCM component names found in the registry. It calls fn
// once per catalog page with the component names of that page. If the cataloger
// supports pagination, last is the last component name of the previous page.
//
// The matching repositories live under a single prefix and, when the catalog
// is sorted (as the Distribution spec requires), form one contiguous block.
// ListComponents starts the scan at that prefix and stops once a sorted page
// proves the block is over, so it does not scan the whole catalog of a large
// registry. Some registries sort under a different collation; the first page
// that is not byte-sorted turns early stopping off, and the scan then reads
// the whole catalog so no component is dropped.
func (l *ComponentLister) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	if fn == nil {
		return ErrFnNil
	}

	prefix := componentDescriptorPrefix(l.subPath)

	// Start at the prefix so repositories before the block are skipped. An empty
	// last starts at the prefix itself; a non-empty last continues after the
	// previous page's last component name.
	cursor := prefix
	if last != "" {
		cursor += last
	}

	earlyStop := true

	err := l.cataloger.Repositories(ctx, cursor, func(repos []string) error {
		// Early stopping relies on byte-sorted pages. The first page that is not
		// sorted turns it off for the rest of the scan, so matches on later pages
		// are not dropped on registries with a different catalog collation.
		if earlyStop && !slices.IsSorted(repos) {
			getLogger().DebugContext(ctx, "catalog page is not byte-sorted; disabling early stop and scanning the full catalog")
			earlyStop = false
		}

		names := make([]string, 0, len(repos))
		for _, repository := range repos {
			if name, ok := strings.CutPrefix(repository, prefix); ok {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			if err := fn(names); err != nil {
				return err
			}
		}

		// Stop only when a sorted page ends past the block: its last entry is not
		// under the prefix and sorts after it, so no later page can hold a match.
		if earlyStop && len(repos) > 0 {
			lastRepo := repos[len(repos)-1]
			if !strings.HasPrefix(lastRepo, prefix) && lastRepo > prefix {
				return errBlockComplete
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errBlockComplete) {
		return err
	}
	return nil
}

func getLogger() *slog.Logger {
	return slog.Default().With(slog.String("realm", "oci-catalog-lister"))
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
