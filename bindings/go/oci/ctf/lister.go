package oci

import (
	"context"
	"fmt"
	"log/slog"
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

// NewComponentLister creates a new ComponentLister for the given CTF archive.
func NewComponentLister(archive ctf.CTF) (*CTFComponentLister, error) {
	lister := &CTFComponentLister{
		archive: archive,
	}

	return lister, nil
}

// ListComponents lists all unique component names found in the CTF archive. The order of the elements
// is determined by the underlying implementation of the store.
// The function does not support pagination and returns the complete list at once.
// Thus, the `last` parameter is ignored.
func (l *CTFComponentLister) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	if last != "" {
		l.log(ctx, "pagination is not supported, ignoring 'last' parameter", "last", last)
	}

	names, err := l.getAllNames(ctx)
	if err != nil {
		return fmt.Errorf("unable to list components: %w", err)
	}

	if err = fn(names); err != nil {
		return fmt.Errorf("callback function returned an error: %w", err)
	}

	return nil
}

func (l *CTFComponentLister) getAllNames(ctx context.Context) ([]string, error) {
	idx, err := l.archive.GetIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get CTF index: %w", err)
	}

	arts := idx.GetArtifacts()
	if len(arts) == 0 {
		return []string{}, nil
	}

	seen := make(map[string]struct{})
	unsortedNames := []string{}
	for _, art := range arts {
		// If repository starts with "component-descriptors/", the rest is the component name.
		prefix := ocipath.DefaultComponentDescriptorPath + "/"
		comp := art.Repository

		if !strings.HasPrefix(comp, prefix) {
			continue
		}
		comp = strings.TrimPrefix(comp, prefix)

		if !seen[comp] {
			seen[comp] = true
			unsortedNames = append(unsortedNames, comp)
		}
	}

	return unsortedNames, nil
}

func (l *CTFComponentLister) log(ctx context.Context, msg string, args ...any) {
	slog.Default().With(slog.String("realm", "ctf-lister")).InfoContext(ctx, msg, args...)
}
