package tui

import (
	"context"
	"fmt"
	"log/slog"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/transfer"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/tui/fetch"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

// newFetcherFactory returns a FetcherFactory that creates fetchers from
// component reference strings using the given OCM context.
func newFetcherFactory(
	pluginManager *manager.PluginManager,
	credentialGraph credentials.Resolver,
	config *genericv1.Config,
) fetch.FetcherFactory {
	return func(ctx context.Context, reference string) (fetch.ComponentFetcher, string, string, error) {
		ref, err := compref.Parse(reference, compref.IgnoreSemverCompatibility())
		if err != nil {
			return nil, "", "", fmt.Errorf("invalid reference %q: %w", reference, err)
		}

		repoResolver, err := ocm.NewComponentVersionRepositoryForComponentProvider(
			ctx,
			pluginManager.ComponentVersionRepositoryRegistry,
			credentialGraph,
			config,
			ref,
		)
		if err != nil {
			return nil, "", "", fmt.Errorf("initializing repository: %w", err)
		}

		fetcher := &componentFetcher{repoResolver: repoResolver}
		return fetcher, ref.Component, ref.Version, nil
	}
}

// componentFetcher implements fetch.ComponentFetcher using the OCM plugin
// manager and credential graph.
type componentFetcher struct {
	repoResolver resolvers.ComponentVersionRepositoryResolver
}

func (f *componentFetcher) ListVersions(ctx context.Context, component string) ([]string, error) {
	repo, err := f.repoResolver.GetComponentVersionRepositoryForComponent(ctx, component, "")
	if err != nil {
		return nil, fmt.Errorf("getting repository for component %s: %w", component, err)
	}
	versions, err := repo.ListComponentVersions(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("listing versions for %s: %w", component, err)
	}
	return versions, nil
}

func (f *componentFetcher) GetDescriptor(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	repo, err := f.repoResolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting repository for %s:%s: %w", component, version, err)
	}
	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting component version %s:%s: %w", component, version, err)
	}
	return desc, nil
}

// transferExecutor implements fetch.TransferExecutor using the OCM transfer package.
type transferExecutor struct {
	pluginManager   *manager.PluginManager
	credentialGraph credentials.Resolver
	config          *genericv1.Config
}

func newTransferExecutor(
	pluginManager *manager.PluginManager,
	credentialGraph credentials.Resolver,
	config *genericv1.Config,
) *transferExecutor {
	return &transferExecutor{
		pluginManager:   pluginManager,
		credentialGraph: credentialGraph,
		config:          config,
	}
}

func (t *transferExecutor) BuildGraph(ctx context.Context, source, target string, opts fetch.TransferOptions) (*transformv1alpha1.TransformationGraphDefinition, error) {
	fromSpec, err := compref.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("invalid source reference: %w", err)
	}

	repoProvider, err := ocm.NewComponentRepositoryResolver(
		ctx, t.pluginManager.ComponentVersionRepositoryRegistry, t.credentialGraph,
		ocm.WithConfig(t.config), ocm.WithComponentRef(fromSpec),
	)
	if err != nil {
		return nil, fmt.Errorf("initializing source repository: %w", err)
	}

	toSpec, err := compref.ParseRepository(target,
		compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid target repository: %w", err)
	}

	copyMode := transfer.CopyModeLocalBlobResources
	if opts.CopyResources {
		copyMode = transfer.CopyModeAllResources
	}

	uploadType := transfer.UploadAsDefault
	switch opts.UploadAs {
	case "localBlob":
		uploadType = transfer.UploadAsLocalBlob
	case "ociArtifact":
		uploadType = transfer.UploadAsOciArtifact
	}

	tgd, err := transfer.BuildGraphDefinition(
		ctx,
		transfer.WithTransfer(
			transfer.Component(fromSpec.Component, fromSpec.Version),
			transfer.ToRepositorySpec(toSpec),
			transfer.FromResolver(repoProvider),
		),
		transfer.WithRecursive(opts.Recursive),
		transfer.WithCopyMode(copyMode),
		transfer.WithUploadType(uploadType),
	)
	if err != nil {
		return nil, fmt.Errorf("building graph: %w", err)
	}

	return tgd, nil
}

func (t *transferExecutor) Execute(ctx context.Context, tgd *transformv1alpha1.TransformationGraphDefinition, progressCh chan<- fetch.TransferProgress) error {
	b := transfer.NewDefaultBuilder(
		t.pluginManager.ComponentVersionRepositoryRegistry,
		t.pluginManager.ResourcePluginRegistry,
		t.credentialGraph,
	)

	eventCh := make(chan graphRuntime.ProgressEvent, 16)
	graph, err := b.WithEvents(eventCh).BuildAndCheck(tgd)
	if err != nil {
		close(progressCh)
		return fmt.Errorf("building transformation graph: %w", err)
	}

	nodeCount := graph.NodeCount()

	// Intercept slog to send log messages to the TUI.
	fwd := &slogForwarder{
		fallback: slog.Default().Handler(),
		ch:       progressCh,
	}
	slog.SetDefault(slog.New(fwd))

	// Forward progress events.
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		completed := 0
		for event := range graph.Events() {
			if event.State == graphRuntime.Completed || event.State == graphRuntime.Failed {
				completed++
			}
			name := event.State.String()
			if event.Transformation != nil {
				name = fmt.Sprintf("%s [%s]: %s", event.Transformation.ID, event.Transformation.Type.Name, event.State.String())
			}
			progressCh <- fetch.TransferProgress{
				Step:    name,
				Total:   nodeCount,
				Current: completed,
			}
		}
	}()

	processErr := graph.Process(ctx)

	// Wait for the event forwarding goroutine to finish.
	<-eventsDone

	// Restore slog BEFORE closing progressCh so no slog writes hit a closed channel.
	slog.SetDefault(slog.New(fwd.fallback))
	fwd.stop()

	close(progressCh)

	if processErr != nil {
		return fmt.Errorf("transfer failed: %w", processErr)
	}

	return nil
}

// slogForwarder is a slog.Handler that sends log records to the TUI progress channel.
type slogForwarder struct {
	fallback slog.Handler
	ch       chan<- fetch.TransferProgress
	stopped  bool
}

func (h *slogForwarder) stop() {
	h.stopped = true
}

func (h *slogForwarder) Enabled(_ context.Context, _ slog.Level) bool { return !h.stopped }

func (h *slogForwarder) Handle(_ context.Context, r slog.Record) error {
	if h.stopped {
		return nil
	}
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		msg += fmt.Sprintf(" %s=%v", a.Key, a.Value)
		return true
	})

	// Non-blocking send — drop if channel is full to avoid deadlock.
	select {
	case h.ch <- fetch.TransferProgress{Step: msg, IsLog: true}:
	default:
	}
	return nil
}

func (h *slogForwarder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogForwarder{fallback: h.fallback.WithAttrs(attrs), ch: h.ch, stopped: h.stopped}
}

func (h *slogForwarder) WithGroup(name string) slog.Handler {
	return &slogForwarder{fallback: h.fallback.WithGroup(name), ch: h.ch, stopped: h.stopped}
}
