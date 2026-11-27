package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	ocipath "ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
	repo "ocm.software/open-component-model/bindings/go/repository"
)

// ErrFnNil reports that ListComponents was called without a callback function.
var ErrFnNil = errors.New("expected a valid callback function, but got nil")

// ErrCatalogScanLimit reports that the scan read the configured number of
// catalog pages without finding any matching repository. It usually means the
// sub-path is wrong or the registry does not honour the catalog cursor.
var ErrCatalogScanLimit = errors.New("catalog scan limit reached")

// defaultScanPageLimit is the number of leading catalog pages scanned before
// the first match before giving up. At the default page size (100) this is
// about 2000 repositories.
const defaultScanPageLimit = 20

// ComponentLister implements repository.ComponentLister for remote OCI
// registries. It gets raw repository names from a RepositoryCataloger and maps
// them to OCM component names. The mapping keeps repositories that contain the
// component-descriptors/ segment, strips the optional subPath prefix and the
// component-descriptors/ prefix, and returns the remainder as the component
// name.
type ComponentLister struct {
	cataloger     RepositoryCataloger
	subPath       string
	scanPageLimit int
}

var _ repo.ComponentLister = (*ComponentLister)(nil)

// ComponentListerOption configures a ComponentLister.
type ComponentListerOption func(*ComponentLister)

// WithScanPageLimit sets how many leading catalog pages are scanned before the
// first match before returning ErrCatalogScanLimit. A value <= 0 keeps the
// default.
func WithScanPageLimit(pages int) ComponentListerOption {
	return func(l *ComponentLister) {
		if pages > 0 {
			l.scanPageLimit = pages
		}
	}
}

// NewComponentLister creates a ComponentLister for the given cataloger and
// subPath. subPath is the OCM repository prefix and may be empty.
func NewComponentLister(c RepositoryCataloger, subPath string, opts ...ComponentListerOption) *ComponentLister {
	l := &ComponentLister{
		cataloger:     c,
		subPath:       subPath,
		scanPageLimit: defaultScanPageLimit,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// errBlockComplete stops the catalog scan once it has passed the block of
// matching repositories. It is swallowed by ListComponents.
var errBlockComplete = errors.New("component descriptor block complete")

// errScanLimit stops the catalog scan once the page cap is reached with no
// match seen yet. It is turned into ErrCatalogScanLimit by ListComponents.
var errScanLimit = errors.New("catalog scan page cap reached")

// blockState tracks where the scan is relative to the contiguous block of
// matching repositories.
type blockState int

const (
	beforeBlock blockState = iota
	inBlock
	afterBlock
)

// ListComponents lists OCM component names found in the registry. It calls fn
// once per catalog page with the component names of that page.
//
// The matching repositories share one prefix and are contiguous in the
// registry's own catalog order. This holds for byte order, segment-wise order
// and the owner-first order some registries (such as ghcr.io) use, because all
// matches share the leading owner and prefix. ListComponents starts the scan
// at the prefix and stops once it has passed the block, so it does not scan the
// whole catalog of a large registry. If the block turns out not to be
// contiguous, early stopping is turned off and the whole catalog is scanned so
// no component is dropped.
//
// When no match is seen within the first scanPageLimit pages, ListComponents
// stops with ErrCatalogScanLimit instead of scanning an entire large registry.
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

	limit := l.scanPageLimit
	if limit <= 0 {
		limit = defaultScanPageLimit
	}

	state := beforeBlock
	earlyStop := true
	pages := 0
	scanLimitHit := false

	err := l.cataloger.Repositories(ctx, cursor, func(repos []string) error {
		pages++

		names := make([]string, 0, len(repos))
		for _, repository := range repos {
			if name, ok := strings.CutPrefix(repository, prefix); ok {
				// A match after the block has ended means the block is not
				// contiguous in this registry's order. Disable early stopping and
				// scan everything so no match is dropped.
				if state == afterBlock {
					if earlyStop {
						getLogger().DebugContext(ctx, "catalog block is not contiguous; disabling early stop and scanning the full catalog")
						earlyStop = false
					}
				} else {
					state = inBlock
				}
				names = append(names, name)
				continue
			}
			// A non-match while inside the block marks its end.
			if state == inBlock {
				state = afterBlock
			}
		}

		if len(names) > 0 {
			if err := fn(names); err != nil {
				return err
			}
		}

		if earlyStop && state == afterBlock {
			return errBlockComplete
		}
		// No match seen yet: stop after the page cap instead of scanning a whole
		// large registry for a prefix that may not exist.
		if state == beforeBlock && pages >= limit {
			scanLimitHit = true
			return errScanLimit
		}
		return nil
	})

	if scanLimitHit {
		return fmt.Errorf("%w: no components found under %q within the first %d catalog pages; the sub-path may be wrong or the registry may not honour the catalog cursor",
			ErrCatalogScanLimit, prefix, limit)
	}
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
