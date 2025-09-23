package oci

import (
	"context"
	"fmt"
	"log/slog"
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

	// options holds the configuration options for the lister.
	options repo.ComponentListerOptions
}

var _ repo.ComponentLister = (*CTFComponentLister)(nil)

// NewComponentLister creates a new ComponentLister for the given CTF archive.
func NewComponentLister(archive ctf.CTF, opts ...repo.ComponentListerOption) (*CTFComponentLister, error) {
	lister := &CTFComponentLister{
		archive: archive,
	}

	for _, opt := range opts {
		opt(&lister.options)
	}

	if lister.options.Logger == nil {
		lister.options.Logger = slog.Default().With("realm", "ctf-lister")
	}

	return lister, nil
}

// ListComponents lists all unique component names found in the CTF archive.
// If SortAlphabetically option is set, the names are sorted alphabetically.
// The function does not support pagination and returns the complete list at once.
// Thus, the `last` parameter and the `NameListPageSize` listing option are ignored.
func (l *CTFComponentLister) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	if l.options.NameListPageSize > 0 {
		l.logInfo(ctx, "pagination is not supported, ignoring page size option", "pageSize", l.options.NameListPageSize)
	}

	if last != "" {
		l.logInfo(ctx, "pagination is not supported, ignoring 'last' parameter", "last", last)
	}

	names, err := l.getAllNames(ctx)
	if err != nil {
		return fmt.Errorf("unable to list components: %w", err)
	}

	if l.options.SortAlphabetically {
		slices.Sort(names)
	}

	if err = fn(names); err != nil {
		return fmt.Errorf("callback function returned an error: %w", err)
	}

	return nil
}

func (l *CTFComponentLister) getAllNames(ctx context.Context) ([]string, error) {
	idx, err := l.archive.GetIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get idx: %w", err)
	}

	arts := idx.GetArtifacts()
	if len(arts) == 0 {
		return []string{}, nil
	}

	seen := make(map[string]bool)
	unsortedNames := []string{}
	for _, art := range arts {
		// If repository starts with "component-descriptors/", the rest is the component name.
		prefix := ocipath.DefaultComponentDescriptorPath + "/"
		comp := art.Repository

		if strings.HasPrefix(comp, prefix) {
			comp = strings.TrimPrefix(comp, prefix)
		} else {
			continue
		}

		if !seen[comp] {
			seen[comp] = true
			unsortedNames = append(unsortedNames, comp)
		}
	}

	return unsortedNames, nil
}

func (l *CTFComponentLister) logInfo(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelInfo, msg, args...)
}

func (l *CTFComponentLister) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	logger := l.options.Logger
	if logger != nil {
		logger.Log(ctx, level, msg, args...)
	}
}
