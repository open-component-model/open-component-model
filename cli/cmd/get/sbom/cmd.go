package sbom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	goruntime "runtime"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	ocmblob "ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artefactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artefactref/v1"
	"ocm.software/open-component-model/bindings/go/oci/attestation"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagResourceIdentity = "identity"
	FlagOutput           = "output"
)

const (
	platformAttributeOS           = "os"
	platformAttributeArchitecture = "architecture"
	platformAttributeVariant      = "variant"
)

// resourceTypeSBOM is the resource type an SBOM is published under.
const resourceTypeSBOM = "sbom"

// errNotFound used to signal that the next strategy needs to happen.
var errNotFound = errors.New("no sbom found")

const (
	// OutputFormatJSON re-indents the discovered document before printing it.
	OutputFormatJSON = "json"
	// OutputFormatRaw prints the document exactly as it is stored.
	OutputFormatRaw = "raw"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "sbom {reference}",
		Aliases:           []string{"sboms"},
		Short:             "Get the SBOM describing a resource of a component version",
		Args:              cobra.ExactArgs(1),
		RunE:              GetSBOM,
		DisableAutoGenTag: true,
		Long: `Get the Software Bill of Materials describing a resource of a component version.

The resource is selected with --identity. Its SBOM is then looked for in two ways, in order:

  1. Another resource of the same component version declaring, through the
     "` + artefactref.LabelName + `" label, that it describes the selected resource.
  2. For a resource backed by an OCI artifact, an SBOM attached to that artifact by
     "docker buildx build --sbom=true". SBOMs attached by other tooling, such as cosign
     or the OCI referrers API, are not discovered.

The SBOM document is written to standard output as it was published, so it can be piped
straight into a scanner.`,
		Example: strings.TrimSpace(`
Getting the SBOM of a resource:

ocm get sbom ghcr.io/open-component-model//ocm.software/tutorial-sbom:1.0.0 --identity name=cli
ocm get sbom ./path/to/ctf//ocm.software/tutorial-sbom:1.0.0 --identity name=cli

Selecting one of several builds of the same resource:

ocm get sbom ghcr.io/org//ocm.software/product:1.0.0 --identity name=cli,architecture=arm64

Piping into a scanner:

ocm get sbom ghcr.io/org//ocm.software/product:1.0.0 --identity name=image -o raw > sbom.json
`),
	}

	cmd.Flags().String(FlagResourceIdentity, "", "identity of the resource to get the SBOM for")
	_ = cmd.MarkFlagRequired(FlagResourceIdentity)
	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{OutputFormatJSON, OutputFormatRaw},
		"output format of the SBOM document. 'raw' writes the document byte for byte, "+
			"which is what any signature or digest over it covers")

	return cmd
}

func GetSBOM(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}

	identityStr, err := cmd.Flags().GetString(FlagResourceIdentity)
	if err != nil {
		return fmt.Errorf("getting identity flag failed: %w", err)
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	requestedIdentity, err := runtime.ParseIdentity(identityStr)
	if err != nil {
		return fmt.Errorf("parsing resource identity %q failed: %w", identityStr, err)
	}

	reference := args[0]
	ref, err := compref.Parse(reference)
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", reference, err)
	}

	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(ctx, pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, nil, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx, ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("getting component version failed: %w", err)
	}

	target, err := selectResource(desc, requestedIdentity)
	if err != nil {
		return err
	}

	document, err := discoverSBOMs(ctx, logger, pluginManager, credentialGraph,
		componentVersion{desc: desc, repo: repo, ref: ref}, target,
		attestation.WithPlatform(ociImageSpecV1.Platform{
			OS:           requestedIdentity[platformAttributeOS],
			Architecture: requestedIdentity[platformAttributeArchitecture],
			Variant:      requestedIdentity[platformAttributeVariant],
		}))
	if err != nil {
		return err
	}

	return write(cmd, output, document)
}

// componentVersion is a convenience to wrap several parameter items.
type componentVersion struct {
	desc *descriptor.Descriptor
	repo repository.ComponentVersionRepository
	ref  *compref.Ref
}

// discoverSBOMs runs the lookup strategies in order and returns the SBOM document.
func discoverSBOMs(
	ctx context.Context,
	logger *slog.Logger,
	pluginManager *manager.PluginManager,
	credentialGraph credentials.Resolver,
	version componentVersion,
	target *descriptor.Resource,
	opts ...attestation.Option,
) ([]byte, error) {
	document, err := discoverForArtefactReferences(ctx, logger, pluginManager, credentialGraph, version, target)
	if err == nil {
		return document, nil
	}
	if !errors.Is(err, errNotFound) {
		return nil, err
	}

	logger.Debug("no sbom resource references the requested resource, inspecting its artifact",
		slog.String("resource", target.ToIdentity().String()))

	return discoverForOCIArtifacts(ctx, logger, pluginManager, credentialGraph, target, opts...)
}

// discoverForArtefactReferences looks for a resource of the component version
// that declares, through the artefact reference label, that it describes the target.
func discoverForArtefactReferences(
	ctx context.Context,
	logger *slog.Logger,
	pluginManager *manager.PluginManager,
	credentialGraph credentials.Resolver,
	version componentVersion,
	target *descriptor.Resource,
) ([]byte, error) {
	targetIdentity := target.ToIdentity()

	referring, err := artefactref.FindDescribingResources(version.desc, targetIdentity)
	if err != nil && !errors.Is(err, artefactref.ErrNotFound) {
		return nil, err
	}

	// Filter for platforms.
	var matches []*descriptor.Resource
	for _, candidate := range referring {
		switch {
		case candidate.Type != resourceTypeSBOM:
			logger.Debug("ignoring resource referencing the requested resource, it is not an sbom",
				slog.String("resource", candidate.ToIdentity().String()),
				slog.String("type", candidate.Type))
		case !matchesPlatform(candidate, target.ExtraIdentity):
			logger.Debug("ignoring sbom referencing the requested resource, it is for another platform",
				slog.String("sbom", candidate.ToIdentity().String()))
		default:
			matches = append(matches, candidate)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: no resource of type %q describes %q", errNotFound, resourceTypeSBOM, targetIdentity)
	}
	describing := matches[0]

	logger.Info("found an sbom resource referencing the requested resource",
		slog.String("sbom", describing.ToIdentity().String()),
		slog.String("resource", targetIdentity.String()))

	data, err := shared.DownloadResourceData(ctx, pluginManager, credentialGraph, version.ref.Component, version.ref.Version, version.repo, describing, describing.ToIdentity())
	if err != nil {
		return nil, fmt.Errorf("downloading sbom resource %q failed: %w", describing.ToIdentity(), err)
	}
	var buf bytes.Buffer
	if err := ocmblob.Copy(&buf, data); err != nil {
		return nil, fmt.Errorf("reading sbom resource %q failed: %w", describing.ToIdentity(), err)
	}
	return buf.Bytes(), nil
}

// discoverForOCIArtifacts inspects the artifact the resource itself points at, for an
// SBOM attached to it at build time.
func discoverForOCIArtifacts(
	ctx context.Context,
	logger *slog.Logger,
	pluginManager *manager.PluginManager,
	credentialGraph credentials.Resolver,
	target *descriptor.Resource,
	opts ...attestation.Option,
) ([]byte, error) {
	targetIdentity := target.ToIdentity()

	access := target.GetAccess()
	if access == nil {
		return nil, fmt.Errorf("no sbom found for resource %q: nothing in the component version references it, and it has no access to inspect for an attached one", targetIdentity)
	}

	notInspectable := func(reason error) error {
		return fmt.Errorf("no sbom found for resource %q: nothing in the component version references it, and its access type %q cannot be inspected for an attached sbom (%w)",
			targetIdentity, access.GetType(), reason)
	}

	plugin, err := pluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, access)
	if err != nil {
		return nil, notInspectable(err)
	}

	discoverer, ok := plugin.(attestation.SBOMDiscoverer)
	if !ok {
		return nil, notInspectable(fmt.Errorf("%T does not support sbom discovery", plugin))
	}

	var creds runtime.Typed
	if credIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, target); err == nil {
		if creds, err = credentialGraph.Resolve(ctx, credIdentity); err != nil && !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("getting credentials for resource %q failed: %w", targetIdentity, err)
		}
	}

	sboms, err := discoverer.DiscoverSBOM(ctx, target, creds, opts...)
	if err != nil {
		return nil, err
	}

	// Get the core.
	// TODO: This will be contested when I'll do folder approach.
	sbom, ok := attestation.Core(sboms)
	if !ok {
		described := make([]string, 0, len(sboms))
		for _, candidate := range sboms {
			described = append(described, fmt.Sprintf("%q (%s)", candidate.Name, candidate.Layer.Digest))
		}
		return nil, fmt.Errorf("found %d sboms attached to resource %q but none of them describes the image itself, only build stages: %s",
			len(sboms), targetIdentity, strings.Join(described, ", "))
	}

	logger.Info("found an sbom attached to the artifact of the requested resource",
		slog.String("resource", targetIdentity.String()),
		slog.String("predicateType", sbom.PredicateType),
		slog.String("name", sbom.Name),
		slog.Int("discovered", len(sboms)))

	return sbom.Predicate, nil
}

// selectResource picks the resource the SBOM is wanted for.
// This follows a stricter rule as download because the ocm-spec defines more strict extraIdentity matching require.
func selectResource(desc *descriptor.Descriptor, requested runtime.Identity) (*descriptor.Resource, error) {
	var matches []*descriptor.Resource
	for i := range desc.Component.Resources {
		resource := &desc.Component.Resources[i]
		if requested.Match(resource.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			if matchesPlatform(resource, requested) {
				matches = append(matches, resource)
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no resource found matching identity %q", requested)
	}

	return matches[0], nil
}

// matchesPlatform checks if the resource has the requested platform.
// There is a bit of a problem with OS. The sbom will not have os and there needs to be
// a strict match later. So we are going to make this a loose match.
func matchesPlatform(resource *descriptor.Resource, wanted runtime.Identity) bool {
	if want, asked := wanted[platformAttributeOS]; asked {
		if os, declared := resource.ExtraIdentity[platformAttributeOS]; declared && os != want {
			return false
		}
	}
	want, asked := wanted[platformAttributeArchitecture]
	if !asked {
		want = goruntime.GOARCH
	}
	if architecture, declared := resource.ExtraIdentity[platformAttributeArchitecture]; declared && architecture != want {
		return false
	}
	return true
}

func write(cmd *cobra.Command, output string, document []byte) error {
	out := cmd.OutOrStdout()

	switch output {
	case OutputFormatRaw:
		_, err := out.Write(document)
		return err
	case OutputFormatJSON:
		var indented bytes.Buffer
		if err := json.Indent(&indented, document, "", "  "); err != nil {
			return fmt.Errorf("the sbom is not valid json and can only be written with --%s %s: %w", FlagOutput, OutputFormatRaw, err)
		}
		if _, err := out.Write(indented.Bytes()); err != nil {
			return err
		}
		_, err := out.Write([]byte("\n"))
		return err
	default:
		return fmt.Errorf("unsupported output format %q", output)
	}
}
