package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/opencontainers/go-digest"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	resourceName      = "manifest"
	manifestMediaType = "application/x-yaml"
	publishWorkers    = 16
)

// componentVersion is one component version the benchmark pushes, with the
// manifest its single local resource carries.
type componentVersion struct {
	Name     string
	Version  string
	Manifest []byte
}

// publisher pushes component versions to the benchmark registry.
type publisher struct {
	baseURL string
	tempDir string
}

func (p *publisher) publish(ctx context.Context, cvs []componentVersion) error {
	g, ctx := errgroup.WithContext(ctx)
	work := make(chan componentVersion)

	g.Go(func() error {
		defer close(work)
		for _, cv := range cvs {
			select {
			case work <- cv:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	for range publishWorkers {
		g.Go(func() error {
			repo, err := p.repository()
			if err != nil {
				return err
			}
			for cv := range work {
				if err := pushComponentVersion(ctx, repo, cv); err != nil {
					return fmt.Errorf("publishing %s:%s: %w", cv.Name, cv.Version, err)
				}
			}
			return nil
		})
	}

	return g.Wait()
}

func (p *publisher) repository() (*oci.Repository, error) {
	// The resolver takes a bare host as reference prefix; the scheme only
	// decides plain HTTP.
	u, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing registry URL: %w", err)
	}
	resolver, err := urlresolver.New(urlresolver.WithBaseURL(u.Host), urlresolver.WithPlainHTTP(u.Scheme == "http"))
	if err != nil {
		return nil, fmt.Errorf("creating resolver: %w", err)
	}

	scheme := ocmruntime.NewScheme()
	ociaccess.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)

	tmp, err := os.MkdirTemp(p.tempDir, "publish-")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	return oci.NewRepository(oci.WithResolver(resolver), oci.WithScheme(scheme), oci.WithTempDir(tmp))
}

func pushComponentVersion(ctx context.Context, repo *oci.Repository, cv componentVersion) error {
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "ocm.software"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: cv.Name, Version: cv.Version},
			},
		},
	}

	res := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: cv.Version},
		},
		Type: "blob",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(cv.Manifest).String(),
			MediaType:      manifestMediaType,
		},
	}

	added, err := repo.AddLocalResource(ctx, cv.Name, cv.Version, res, inmemory.New(bytes.NewReader(cv.Manifest)))
	if err != nil {
		return fmt.Errorf("adding local resource: %w", err)
	}
	desc.Component.Resources = []descriptor.Resource{*added}

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return fmt.Errorf("adding component version: %w", err)
	}
	return nil
}
