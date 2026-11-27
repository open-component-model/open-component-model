package componentversion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/cli/internal/componentlister"
	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/cli/internal/render"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/graph/list"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/graph/tree"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress/bar"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	versioningspec "ocm.software/open-component-model/bindings/go/configuration/versioning/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/catalog"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

const (
	FlagConstraint       = "constraint"
	FlagOutput           = "output"
	FlagDisplayMode      = "display-mode"
	FlagConcurrencyLimit = "concurrency-limit"
	FlagLatest           = "latest"
	FlagRecursive        = "recursive"
	FlagDetail           = "detail"
)

// errNoComponentsFound reports that a repository reference held no components.
var errNoComponentsFound = errors.New("no components found")

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Get component version(s) from an OCM repository",
		Args:       cobra.MatchAll(cobra.ExactArgs(1), componentOrRepositoryReferenceAsFirstPositional),
		Long: fmt.Sprintf(`Get component version(s) from an OCM repository.

The format of a component reference is:

	[type::]{repository}/[valid-prefix]/{component}[:version][@digest]

For valid prefixes {%[1]s|none} are available. If <none> is used, it defaults to %[1]q. This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {%[2]s} are supported, which can be shortened to {%[3]s} respectively for convenience.

A digest pins the component version: the command fails unless the component version matches it.
Two forms are accepted, each with sha256 or sha512:

- "@sha256:<hex>" (OCI digest) is the digest of the OCI image index or manifest the component version is
  stored as, the digest its version tag resolves to (for example "oras resolve
  {repository}/component-descriptors/{component}:{version}"). The component version is then read from that
  artifact instead of through the tag, like an OCI image pinned by digest. The digest covers the descriptor
  and all local blobs, so the version may be omitted; if given, the artifact must hold it. Only OCI
  registries and CTF archives support it, and it changes when the version is transferred or signed.
- "@jsonNormalisation.v4alpha3+sha256:<hex>" (normalized) is the hash of the descriptor normalised with
  jsonNormalisation/v4alpha3, the digest "ocm sign" signs and logs. It stays the same across transfer and
  signing, but does not cover access information or labels that are excluded from signing. A reference
  pinned to it must name the version.

If no type is given, the repository path is interpreted based on introspection and heuristics.

When the reference is a repository (no component), every component in the repository is listed. Components
that cannot be read (for example missing pull permission or an unsupported descriptor schema) are reported
and skipped; the command still succeeds unless no component can be read.

The --detail level controls how much is fetched for a repository reference:
  names (default): component names only, from the catalog (one request, fast);
  versions: also list the versions of each component (one request per component);
  full: fetch descriptors, needed for --recursive and the tree outputs.
When --detail is not set, the cheapest level that satisfies the other flags is used: --latest or
--constraint select versions; --recursive or -o tree|widetree select full. Listing a CTF archive now
defaults to names too; pass --detail full for the previous descriptor output.

For a component reference without a version (repo//component), --detail versions lists its versions
without fetching descriptors, and full (the default) fetches them. A pinned reference
(repo//component:version or @digest) is always full.
`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
Getting a single component version:

get component-version ghcr.io/open-component-model//ocm.software/cli:0.12.0
get cv ./path/to/ctf//ocm.software/cli:0.12.0
get cv ./path/to/ctf/component-descriptors/ocm.software/cli:0.12.0
get cv ghcr.io/open-component-model//ocm.software/cli@sha256:<oci-digest>
get cv ghcr.io/open-component-model//ocm.software/cli:0.12.0@jsonNormalisation.v4alpha3+sha256:<signed-digest>

Listing components of a repository (no component in the reference):

get cvs ghcr.io/open-component-model                         # component names (fast, default)
get cvs ghcr.io/open-component-model --detail versions       # every version of every component
get cvs ghcr.io/open-component-model --detail versions --latest
get cvs ghcr.io/open-component-model --detail full -o yaml   # descriptors
get cv ghcr.io/open-component-model//ocm.software/cli --detail versions   # versions of one component

Listing many component versions:

get component-versions ghcr.io/open-component-model//ocm.software/cli
get cvs ghcr.io/open-component-model//ocm.software/cli --output json
get cvs ghcr.io/open-component-model//ocm.software/cli -oyaml

Showing the resources of each component version in the tree output:

get cv ./path/to/ctf//ocm.software/cli:0.12.0 -o widetree
get cvs ghcr.io/open-component-model//ocm.software/cli -o widetree --recursive

Specifying types and schemes:

get cv ctf::github.com/locally-checked-out-repo//ocm.software/cli:0.12.0
get cvs oci::localhost:8080//ocm.software/cli

A registry without TLS needs plainHTTP in the HTTP client configuration; an http:// prefix on the
repository is deprecated and fails without it (see https://ocm.software/docs/changelog/migrate-from-http-prefixes/).
`),
		RunE:              GetComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTree.String(), render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), render.OutputFormatWideTree.String()}, "output format of the component descriptors")
	enum.VarP(cmd.Flags(), FlagDisplayMode, "", []string{render.StaticRenderMode, render.LiveRenderMode}, `display mode can be used in combination with --recursive
  static: print the output once the complete component graph is discovered
  live (experimental): continuously updates the output to represent the current discovery state of the component graph`)
	cmd.Flags().String(FlagConstraint, "> 0.0.0-0", "version constraint restricting which versions to output, evaluated by each version's configured scheme; versions with no applicable scheme are retained")
	// TODO(fabianburth): add concurrency limit to the dag discovery (https://github.com/open-component-model/ocm-project/issues/705)
	// cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().Bool(FlagLatest, false, "if set, only the latest version of the component is returned")
	cmd.Flags().Int(FlagRecursive, 0, "depth of recursion for resolving referenced component versions (0=none, -1=unlimited, >0=levels (not implemented yet))")
	cmd.Flags().Lookup(FlagRecursive).NoOptDefVal = "-1"

	enum.VarP(cmd.Flags(), FlagDetail, "", []string{detailNames, detailVersions, detailFull}, `detail level when listing (a repository reference, or a component reference without a version):
  names (default for a repository): component names only, from the catalog (cheapest)
  versions: also list the versions of each component (one request per component)
  full: fetch descriptors; required for --recursive and the tree outputs; the default for a component reference
When unset, the cheapest level that satisfies the other flags is used.`)

	return cmd
}

func componentOrRepositoryReferenceAsFirstPositional(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing component reference as first positional argument")
	}
	var compErr, repoErr error
	if _, compErr = compref.Parse(args[0]); compErr == nil {
		return nil
	}
	if _, repoErr = compref.ParseRepository(args[0]); repoErr == nil {
		return nil
	}
	return fmt.Errorf("first position argument %q must be either a component reference or repository reference: %w", args[0], errors.Join(compErr, repoErr))
}

func GetComponentVersion(cmd *cobra.Command, args []string) error {
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}
	displayMode, err := enum.Get(cmd.Flags(), FlagDisplayMode)
	if err != nil {
		return fmt.Errorf("getting display-mode flag failed: %w", err)
	}
	constraint, err := cmd.Flags().GetString(FlagConstraint)
	if err != nil {
		return fmt.Errorf("getting constraint flag failed: %w", err)
	}
	// TODO(fabianburth): add concurrency limit to the dag discovery (https://github.com/open-component-model/ocm-project/issues/705)
	// concurrencyLimit, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	// if err != nil {
	//	 return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	// }
	latestOnly, err := cmd.Flags().GetBool(FlagLatest)
	if err != nil {
		return fmt.Errorf("getting latest flag failed: %w", err)
	}
	recursive, err := cmd.Flags().GetInt(FlagRecursive)
	if err != nil {
		return fmt.Errorf("getting recursive flag failed: %w", err)
	}
	detail, err := enum.Get(cmd.Flags(), FlagDetail)
	if err != nil {
		return fmt.Errorf("getting detail flag failed: %w", err)
	}

	config := ocmctx.FromContext(cmd.Context()).Configuration()

	params := Params{
		output:        output,
		displayMode:   displayMode,
		constraint:    constraint,
		latestOnly:    latestOnly,
		recursive:     recursive,
		detail:        detail,
		detailSet:     cmd.Flags().Changed(FlagDetail),
		latestSet:     cmd.Flags().Changed(FlagLatest),
		constraintSet: cmd.Flags().Changed(FlagConstraint),
		outputSet:     cmd.Flags().Changed(FlagOutput),
	}

	reference := args[0]

	// We want to allow non-semver compatible versions to be used in component references.
	// Therefore, we ignore semver compatibility errors during parsing here.
	ref, compErr := compref.Parse(reference, []compref.Option{
		compref.IgnoreSemverCompatibility(),
	}...)
	if compErr != nil {
		// If not a component reference, check if it is a repository reference.
		repository, repoErr := compref.ParseRepository(args[0])
		if repoErr != nil {
			return fmt.Errorf("first position argument %q must be either a component reference or repository reference: %w", reference, errors.Join(compErr, repoErr))
		}
		slog.DebugContext(cmd.Context(), "parsed repository reference", "reference", reference, "parsed", repository)

		if err := processRepositoryReference(cmd, pluginManager, credentialGraph, config, params, repository, reference); err != nil {
			// A repository reference that finds nothing, or gives up scanning, very
			// likely meant a component reference missing its "//".
			if hint := componentlister.ComponentHint(reference); hint != "" &&
				(errors.Is(err, errNoComponentsFound) || errors.Is(err, catalog.ErrCatalogScanLimit)) {
				return fmt.Errorf("%w; did you mean %q?", err, hint)
			}
			return err
		}
		return nil
	}
	slog.DebugContext(cmd.Context(), "parsed component reference", "reference", reference, "parsed", ref)

	return processComponentReference(cmd, pluginManager, credentialGraph, config, params, ref)
}

func processComponentReference(cmd *cobra.Command,
	pluginManager *manager.PluginManager,
	credentialGraph credentials.Resolver,
	config *genericv1.Config,
	params Params,
	ref *compref.Ref,
) error {
	constraint := params.constraint
	latestOnly := params.latestOnly
	recursive := params.recursive
	output := params.output
	displayMode := params.displayMode
	ctx := cmd.Context()

	pin, err := ocm.DigestPin(ref)
	if err != nil {
		return err
	}

	// A pinned reference (an explicit version or a digest) names exactly one
	// component version, so only the full level applies. Otherwise honour
	// --detail: versions lists the tags without fetching descriptors.
	level, err := effectiveComponentDetailLevel(detailSelection{
		detailSet: params.detailSet,
		detail:    params.detail,
		recursive: params.recursive,
		output:    params.output,
		outputSet: params.outputSet,
	}, pin != nil || ref.Version != "")
	if err != nil {
		return err
	}

	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(ctx, pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, config, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	// The versions level lists the component's versions without fetching
	// descriptors, in the same flat output as the repository versions level.
	if level == detailVersions {
		registry, err := versioningspec.RegistryFromConfig(config)
		if err != nil {
			return fmt.Errorf("could not build versioning registry: %w", err)
		}
		entries, err := collectComponentVersions(ctx, nil, repoProvider, []string{ref.Component}, params, registry)
		if err != nil {
			return err
		}
		listOutput := output
		if !params.outputSet {
			listOutput = render.OutputFormatTable.String()
		}
		return renderVersions(cmd.OutOrStdout(), listOutput, entries)
	}

	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	var descs []*descruntime.Descriptor
	if pin != nil {
		// A pinned reference names exactly one component version; with an OCI digest it may
		// omit the version, which is then taken from the pinned descriptor.
		_, desc, err := ocm.GetPinnedComponentVersion(ctx, repo, ref)
		if err != nil {
			return fmt.Errorf("getting pinned component version failed: %w", err)
		}
		descs = []*descruntime.Descriptor{desc}
	} else {
		registry, err := versioningspec.RegistryFromConfig(config)
		if err != nil {
			return fmt.Errorf("could not build versioning registry: %w", err)
		}
		if descs, err = ocm.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{
			VersionOptions: ocm.VersionOptions{
				SemverConstraint: constraint,
				LatestOnly:       latestOnly,
				Registry:         registry,
			},
		}, ref.Component, ref.Version, repo); err != nil {
			return fmt.Errorf("getting component reference and versions failed: %w", err)
		}
	}
	roots := make([]string, 0, len(descs))
	for _, desc := range descs {
		identity := runtime.Identity{
			descruntime.IdentityAttributeName:    desc.Component.Name,
			descruntime.IdentityAttributeVersion: desc.Component.Version,
		}.String()
		roots = append(roots, identity)
	}

	// Show the source repository as the top row of the tree output, the same as
	// a repository reference (location and type, without the "<type>::" prefix),
	// so the component versions are drawn under it instead of as orphan roots.
	rootLabel := []string{fmt.Sprintf("%s (%s)", ref.Location(), ref.Repository.GetType().Name)}
	if err := renderComponents(cmd, repoProvider, roots, pin, output, displayMode, recursive, rootLabel); err != nil {
		return fmt.Errorf("failed to render components: %w", err)
	}

	return nil
}

func renderComponents(cmd *cobra.Command, repoResolver resolvers.ComponentVersionRepositoryResolver, roots []string, pin *descruntime.Digest, format string, mode string, recursive int, rootLabel []string) error {
	pinned := make(map[string]*descruntime.Digest, len(roots))
	if pin != nil {
		for _, root := range roots {
			pinned[root] = pin
		}
	}
	resAndDis := resolverAndDiscoverer{
		repositoryResolver: repoResolver,
		recursive:          recursive,
		pinned:             pinned,
	}
	discoverer := syncdag.NewGraphDiscoverer(&syncdag.GraphDiscovererOptions[string, *descruntime.Descriptor]{
		Roots:      roots,
		Resolver:   &resAndDis,
		Discoverer: &resAndDis,
	})
	renderer, err := buildRenderer(cmd.Context(), discoverer.Graph(), roots, format, rootLabel)
	if err != nil {
		return fmt.Errorf("building renderer failed: %w", err)
	}

	switch mode {
	case render.StaticRenderMode:
		// Start traversing the graph from the root vertices (the initially resolved
		// component versions).
		err := discoverer.Discover(cmd.Context())
		if err != nil {
			return fmt.Errorf("traversing component version graph failed: %w", err)
		}
		if err := render.RenderOnce(cmd.Context(), renderer, render.WithWriter(cmd.OutOrStdout())); err != nil {
			return err
		}
	case render.LiveRenderMode:
		// Start the render loop.
		renderCtx, cancel := context.WithCancel(cmd.Context())
		wait := render.RunRenderLoop(renderCtx, renderer, render.WithRenderOptions(render.WithWriter(cmd.OutOrStdout())))
		// Start traversing the graph from the root vertices (the initially resolved
		// component versions).
		// The render loop is running concurrently and regularly displays the current
		// state of the graph.
		err := discoverer.Discover(cmd.Context())
		cancel()
		if err != nil {
			return fmt.Errorf("traversing component version graph failed: %w", err)
		}

		if err := wait(); !errors.Is(err, context.Canceled) {
			return fmt.Errorf("rendering component version graph failed: %w", err)
		}
	}
	return nil
}

func buildRenderer(ctx context.Context, dag *syncdag.SyncedDirectedAcyclicGraph[string], roots []string, format string, rootLabel []string) (render.Renderer, error) {
	// Initialize renderer based on the requested output format.
	switch format {
	case render.OutputFormatJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatJSON))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatNDJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatNDJSON))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatYAML.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatYAML))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatTree.String():
		return tree.New(ctx, dag, tree.WithRoots(roots...), tree.WithRootLabel[string](rootLabel...)), nil
	case render.OutputFormatWideTree.String():
		return tree.New(ctx, dag,
			tree.WithRoots(roots...),
			tree.WithRootLabel[string](rootLabel...),
			tree.WithHeader[string](wideTreeHeader...),
			tree.WithVertexSerializerFunc(serializeVertexToWideTreeRow),
		), nil
	case render.OutputFormatTable.String():
		serializer := list.ListSerializerFunc[string](serializeVerticesToTable)
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

func serializeVertexToDescriptor(vertex *dag.Vertex[string]) (any, error) {
	untypedDescriptor, ok := vertex.Attributes[syncdag.AttributeValue]
	if !ok {
		return nil, fmt.Errorf("vertex %s has no %s attribute", vertex.ID, syncdag.AttributeValue)
	}
	descriptor, ok := untypedDescriptor.(*descruntime.Descriptor)
	if !ok {
		return nil, fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, syncdag.AttributeValue, &descruntime.Descriptor{}, untypedDescriptor)
	}
	descriptorV2, err := descruntime.ConvertToV2(descriptorv2.Scheme, descriptor)
	if err != nil {
		return nil, fmt.Errorf("converting descriptor to v2 failed: %w", err)
	}
	return descriptorV2, nil
}

// wideTreeHeader describes the columns of the widetree output. It renders both
// component versions and the resources they contain, so it carries the columns
// of both element kinds. COMPONENT and PROVIDER are filled for component rows,
// NAME, TYPE and RELATION for resource rows.
var wideTreeHeader = []string{"COMPONENT", "NAME", "VERSION", "PROVIDER", "TYPE", "RELATION", "IDENTITY"}

// extraIdentity returns the identity without the name attribute, because the
// name already has its own column. What remains is the extra identity, which is
// what distinguishes elements that share a name. An element without extra
// identity yields an empty string.
func extraIdentity(identity runtime.Identity) string {
	delete(identity, descruntime.IdentityAttributeName)
	if len(identity) == 0 {
		return ""
	}
	return identity.String()
}

// serializeVertexToWideTreeRow serializes the component version held by the
// vertex into a row matching wideTreeHeader, with one nested row per resource of
// the component version. The nested rows are rendered below the component row.
func serializeVertexToWideTreeRow(vertex *dag.Vertex[string]) (tree.Row, error) {
	untypedDescriptor, ok := vertex.Attributes[syncdag.AttributeValue]
	if !ok {
		return tree.Row{}, fmt.Errorf("vertex %s has no %s attribute", vertex.ID, syncdag.AttributeValue)
	}
	desc, ok := untypedDescriptor.(*descruntime.Descriptor)
	if !ok {
		return tree.Row{}, fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, syncdag.AttributeValue, &descruntime.Descriptor{}, untypedDescriptor)
	}
	resources := make([]tree.Row, 0, len(desc.Component.Resources))
	for i := range desc.Component.Resources {
		res := &desc.Component.Resources[i]
		resources = append(resources, tree.Row{Cells: []string{
			"", // COMPONENT: a resource belongs to the component row above it.
			res.Name,
			res.Version,
			"", // PROVIDER: resources inherit the provider of their component.
			res.Type,
			string(res.Relation),
			extraIdentity(res.ToIdentity()),
		}})
	}
	return tree.Row{
		Cells: []string{
			desc.Component.Name,
			"", // NAME: only resources carry an element name.
			desc.Component.Version,
			desc.Component.Provider.Name,
			"", // TYPE: only resources carry a type.
			"", // RELATION: only resources carry a relation.
			extraIdentity(desc.Component.ToIdentity()),
		},
		Children: resources,
	}, nil
}

func serializeVerticesToTable(writer io.Writer, vertices []*dag.Vertex[string]) error {
	t := table.NewWriter()
	t.SetOutputMirror(writer)
	t.AppendHeader(table.Row{"Component", "Version", "Provider"})
	for _, vertex := range vertices {
		untypedDescriptor, ok := vertex.Attributes[syncdag.AttributeValue]
		if !ok {
			return fmt.Errorf("vertex %s has no %s attribute", vertex.ID, syncdag.AttributeValue)
		}
		descriptor, ok := untypedDescriptor.(*descruntime.Descriptor)
		if !ok {
			return fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, syncdag.AttributeValue, &descruntime.Descriptor{}, descriptor)
		}

		t.AppendRow(table.Row{descriptor.Component.Name, descriptor.Component.Version, descriptor.Component.Provider.Name})
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 3, AutoMerge: true},
	})
	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}

type resolverAndDiscoverer struct {
	repositoryResolver resolvers.ComponentVersionRepositoryResolver
	recursive          int
	// pinned maps root keys to the digest they are pinned to.
	pinned map[string]*descruntime.Digest
}

var (
	_ syncdag.Resolver[string, *descruntime.Descriptor]   = (*resolverAndDiscoverer)(nil)
	_ syncdag.Discoverer[string, *descruntime.Descriptor] = (*resolverAndDiscoverer)(nil)
)

func (r *resolverAndDiscoverer) Resolve(ctx context.Context, key string) (*descruntime.Descriptor, error) {
	id, err := runtime.ParseIdentity(key)
	if err != nil {
		return nil, fmt.Errorf("parsing identity %q failed: %w", key, err)
	}
	component, version := id[descruntime.IdentityAttributeName], id[descruntime.IdentityAttributeVersion]
	repo, err := r.repositoryResolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for identity %q failed: %w", id, err)
	}
	_, desc, err := repository.GetPinnedComponentVersion(ctx, repo, component, version, r.pinned[key])
	if err != nil {
		return nil, fmt.Errorf("getting component version for identity %q failed: %w", id, err)
	}
	return desc, nil
}

func (r *resolverAndDiscoverer) Discover(ctx context.Context, parent *descruntime.Descriptor) ([]string, error) {
	// TODO(fabianburth): Recursion depth has to be implemented as option for
	//  the dag discovery. Once that's implemented, we can pass the recursion
	//  depth to the discovery and remove this check here (https://github.com/open-component-model/ocm-project/issues/706).
	switch {
	case r.recursive < -1:
		return nil, fmt.Errorf("invalid recursion depth %d: must be -1 (unlimited) or >= 0", r.recursive)
	case r.recursive == 0:
		slog.DebugContext(ctx, "not discovering children, recursion depth 0", "component", parent.Component.ToIdentity().String())
		return nil, nil
	case r.recursive == -1:
		// unlimited recursion
		children := make([]string, len(parent.Component.References))
		for index, reference := range parent.Component.References {
			children[index] = reference.ToComponentIdentity().String()
		}
		slog.DebugContext(ctx, "discovering children", "component", parent.Component.ToIdentity().String(), "children", children)
		return children, nil
	case r.recursive > 0:
		return nil, fmt.Errorf("recursion depth > 0 not implemented yet")
	}
	return nil, fmt.Errorf("invalid recursion depth %d", r.recursive)
}

// Params holds the values of the flags for the `get cv` command.
type Params struct {
	output      string
	displayMode string
	constraint  string
	latestOnly  bool
	recursive   int
	// detail is the raw --detail value; detailSet reports whether it was given.
	detail        string
	detailSet     bool
	latestSet     bool
	constraintSet bool
	outputSet     bool
}

// processRepositoryReference implements the logic for the `get cv <ref>` command, where <ref> is a repository reference.
func processRepositoryReference(cmd *cobra.Command,
	pluginManager *manager.PluginManager,
	credentialGraph credentials.Resolver,
	config *genericv1.Config,
	params Params,
	repository runtime.Typed,
	reference string,
) error {
	ctx := cmd.Context()

	level, err := effectiveDetailLevel(detailSelection{
		detailSet:     params.detailSet,
		detail:        params.detail,
		latestSet:     params.latestSet,
		constraintSet: params.constraintSet,
		recursive:     params.recursive,
		output:        params.output,
		outputSet:     params.outputSet,
	})
	if err != nil {
		return err
	}

	// The names and versions levels are flat lists. The default output format
	// (tree) is for the component graph, so fall back to a table for them when
	// the user did not pick a format.
	listOutput := params.output
	if !params.outputSet {
		listOutput = render.OutputFormatTable.String()
	}

	componentNames, err := componentlister.List(ctx, pluginManager, credentialGraph, repository, reference)
	if err != nil {
		return err
	}
	slog.DebugContext(ctx, "listed components from repository", "repository", repository, "components", componentNames, "detail", level)

	if len(componentNames) == 0 {
		return fmt.Errorf("%w in repository %q", errNoComponentsFound, reference)
	}

	// The names level is a single catalog request; print the names and stop.
	if level == detailNames {
		return renderNames(cmd.OutOrStdout(), listOutput, componentNames)
	}

	registry, err := versioningspec.RegistryFromConfig(config)
	if err != nil {
		return fmt.Errorf("could not build versioning registry: %w", err)
	}

	// Both the versions and the full level resolve each component through the
	// same credential-bound resolver.
	repoProvider, err := ocm.NewComponentRepositoryResolver(ctx,
		pluginManager.ComponentVersionRepositoryRegistry,
		credentialGraph,
		ocm.WithConfig(config),
		ocm.WithRepository(repository),
		ocm.WithComponentPatterns(componentNames))
	if err != nil {
		return fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	// The versions and full levels do per-component work; show progress on
	// stderr so stdout stays clean for -o json | jq. The tracker falls back to
	// slog lines for non-terminal output.
	tracker := progress.NewTracker(ctx, cmd.ErrOrStderr(), bar.NewVisualizer[string])
	defer tracker.Stop()

	// The versions level lists the versions of each component without fetching
	// descriptors.
	if level == detailVersions {
		entries, err := collectComponentVersions(ctx, tracker, repoProvider, componentNames, params, registry)
		if err != nil {
			return err
		}
		tracker.Stop() // restore slog and clear the bar before writing the result
		return renderVersions(cmd.OutOrStdout(), listOutput, entries)
	}

	// The full level fetches descriptors, supports recursion and the tree
	// outputs, and renders the component graph.
	roots, err := getIDsForComponentsFromRepository(ctx, tracker, repoProvider, componentNames, params, registry)
	if err != nil {
		return fmt.Errorf("failed to get identities for components in repository %q: %w", reference, err)
	}
	tracker.Stop() // restore slog and clear the bar before rendering
	slog.DebugContext(ctx, "components versions", "roots", roots)

	// Show the source repository as the top row of the tree output.
	rootLabel := []string{fmt.Sprintf("%s (%s)", reference, repository.GetType().Name)}
	if err := renderComponents(cmd, repoProvider, roots, nil, params.output, params.displayMode, params.recursive, rootLabel); err != nil {
		return fmt.Errorf("failed to render components recursively: %w", err)
	}

	return nil
}

// getIDsForComponentsFromRepository resolves the component versions of the
// listed components and returns their identities. All components are resolved
// through repoProvider, which applies credentials from the credential graph.
//
// It runs in two concurrent phases, each bounded to listConcurrency workers
// (so at most listConcurrency requests are in flight at a time): first it lists
// the versions (tags) of every component, then it fetches the descriptor of
// every selected version. When tracker is non-nil, each phase reports progress.
//
// Components that cannot be read are reported at ERROR level and skipped: a
// catalog may list components the caller cannot pull, or in an unsupported
// descriptor schema. It returns an error only when no component could be read.
func getIDsForComponentsFromRepository(ctx context.Context,
	tracker *progress.Tracker[string],
	repoProvider resolvers.ComponentVersionRepositoryResolver,
	componentNames []string,
	params Params,
	registry *versioning.Registry,
) ([]string, error) {
	type componentVersions struct {
		name     string
		repo     repository.ComponentVersionRepository
		versions []string
	}

	var (
		mu         sync.Mutex
		listed     []componentVersions
		skipped    = map[string]struct{}{}
		failures   []error
		underlying []error
	)
	skip := func(name string, err error) {
		mu.Lock()
		defer mu.Unlock()
		slog.ErrorContext(ctx, "skipping component that could not be read", "component", name, "error", err)
		if _, ok := skipped[name]; !ok {
			skipped[name] = struct{}{}
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
			underlying = append(underlying, err)
		}
	}

	// Phase 1: list the versions of every component.
	listPhase := startPhase(tracker, "Listing component versions", len(componentNames))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(listConcurrency)
	for _, name := range componentNames {
		eg.Go(func() error {
			listPhase.emit(name, progress.Running, nil)
			repo, err := repoProvider.GetComponentVersionRepositoryForComponent(egCtx, name, "")
			if err != nil {
				skip(name, err)
				listPhase.emit(name, progress.Failed, err)
				return nil
			}
			versions, err := ocm.VersionsWithFiltering(egCtx, name, repo, ocm.VersionOptions{
				SemverConstraint: params.constraint,
				LatestOnly:       params.latestOnly,
				Registry:         registry,
			})
			if err != nil {
				skip(name, err)
				listPhase.emit(name, progress.Failed, err)
				return nil
			}
			if len(versions) == 0 {
				err := errors.New("no matching versions")
				skip(name, err)
				listPhase.emit(name, progress.Failed, err)
				return nil
			}
			mu.Lock()
			listed = append(listed, componentVersions{name: name, repo: repo, versions: versions})
			mu.Unlock()
			listPhase.emit(name, progress.Completed, nil)
			return nil
		})
	}
	err := eg.Wait()
	listPhase.finish()
	if err != nil {
		return nil, err
	}

	// Flatten the selected (component, version) pairs.
	type target struct {
		name    string
		version string
		repo    repository.ComponentVersionRepository
	}
	var targets []target
	for _, cv := range listed {
		for _, v := range cv.versions {
			targets = append(targets, target{name: cv.name, version: v, repo: cv.repo})
		}
	}

	// Phase 2: fetch the descriptor of every selected version. This is the slow
	// part on a large registry.
	var descriptors []*descruntime.Descriptor
	success := map[string]int{}
	fetchPhase := startPhase(tracker, "Fetching component descriptors", len(targets))
	eg2, eg2Ctx := errgroup.WithContext(ctx)
	eg2.SetLimit(listConcurrency)
	for _, tgt := range targets {
		eg2.Go(func() error {
			id := tgt.name + ":" + tgt.version
			fetchPhase.emit(id, progress.Running, nil)
			desc, err := tgt.repo.GetComponentVersion(eg2Ctx, tgt.name, tgt.version)
			if err != nil {
				slog.ErrorContext(ctx, "skipping component version that could not be read",
					"component", tgt.name, "version", tgt.version, "error", err)
				fetchPhase.emit(id, progress.Failed, err)
				return nil
			}
			cid := desc.Component.ToIdentity()
			if len(cid) == 0 || cid.String() == "" {
				slog.ErrorContext(ctx, "skipping component version with an empty identity",
					"component", tgt.name, "version", tgt.version)
				fetchPhase.emit(id, progress.Failed, errors.New("empty component identity"))
				return nil
			}
			mu.Lock()
			descriptors = append(descriptors, desc)
			success[tgt.name]++
			mu.Unlock()
			fetchPhase.emit(id, progress.Completed, nil)
			return nil
		})
	}
	err = eg2.Wait()
	fetchPhase.finish()
	if err != nil {
		return nil, err
	}

	// A component whose versions listed but whose descriptors all failed is
	// skipped, counted once.
	for _, cv := range listed {
		if success[cv.name] == 0 {
			skip(cv.name, errors.New("no readable versions"))
		}
	}

	if len(descriptors) == 0 {
		if len(failures) > 0 {
			return nil, aggregateComponentFailures(failures, underlying)
		}
		return nil, nil
	}

	if len(skipped) > 0 {
		skippedList := make([]string, 0, len(skipped))
		for _, n := range componentNames {
			if _, ok := skipped[n]; ok {
				skippedList = append(skippedList, n)
			}
		}
		slog.WarnContext(ctx, fmt.Sprintf("skipped %d of %d components that could not be read", len(skipped), len(componentNames)), "skipped", skippedList)
	}

	// Order the roots by component name, then version newest first, with the
	// registry built from the OCM configuration so custom version schemes apply.
	// Under the repository row this reads as the contents of the repository, the
	// same order as the versions table.
	if err := ocm.SortDescriptorsByComponentThenVersion(registry, descriptors); err != nil {
		return nil, err
	}

	identities := make([]string, 0, len(descriptors))
	for _, desc := range descriptors {
		identities = append(identities, desc.Component.ToIdentity().String())
	}
	return identities, nil
}
