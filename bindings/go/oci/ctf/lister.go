package ctf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"ocm.software/open-component-model/bindings/go/ctf"
	ocipath "ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
	repo "ocm.software/open-component-model/bindings/go/repository"
)

// CTFComponentLister implements ComponentLister interface for CTF archives.
// It does not support pagination and always returns the complete list of component names.
type CTFComponentLister struct {
	// archive is the CTF store that is able to handle CTF contents.
	archive ctf.CTF
}

var _ repo.ComponentLister = (*CTFComponentLister)(nil)

var ErrFnNil = errors.New("expected a valid callback function, but got nil")

// NewComponentLister creates a new ComponentLister for the given CTF archive.
func NewComponentLister(archive ctf.CTF) *CTFComponentLister {
	lister := &CTFComponentLister{
		archive: archive,
	}

	return lister
}

// ListComponents lists all unique component names found in the CTF archive. List elements are lexically sorted.
// The function does not support pagination and returns the complete list at once.
// Thus, the `last` parameter is ignored.
func (l *CTFComponentLister) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	if fn == nil {
		return ErrFnNil
	}

	if last != "" {
		logger := getLogger()
		logger.DebugContext(ctx, "pagination is not supported, ignoring 'last' parameter", "last", last)
	}

	names, err := l.getAllNames(ctx)
	if err != nil {
		return fmt.Errorf("unable to list components: %w", err)
	}

	return fn(names)
}

func (l *CTFComponentLister) getAllNames(ctx context.Context) ([]string, error) {
	idx, err := l.archive.GetIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get CTF index: %w", err)
	}

	arts := idx.GetArtifacts()
	if len(arts) == 0 {
		return nil, nil
	}

	accumulatedNames := make(map[string]struct{})
	for _, art := range arts {
		// If repository starts with "component-descriptors/", the rest is the component name.
		prefix := ocipath.DefaultComponentDescriptorPath + "/"
		comp := art.Repository

		if !strings.HasPrefix(comp, prefix) {
			continue
		}
		comp = strings.TrimPrefix(comp, prefix)
		accumulatedNames[comp] = struct{}{}
	}

	nameList := slices.Collect(maps.Keys(accumulatedNames))
	slices.Sort(nameList)

	return nameList, nil
}

func getLogger() *slog.Logger {
	return slog.Default().With(slog.String("realm", "ctf-lister"))
}
