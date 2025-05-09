package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	goruntime "runtime"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

var GetComponentVersionCmd = &cobra.Command{
	Use:        "component-version {reference}",
	Aliases:    []string{"cv", "component-versions", "cvs"},
	SuggestFor: []string{"component", "components", "version", "versions"},
	Short:      "Get component version(s) from an OCM repository",
	// GroupID:    "component",
	Args: cobra.MatchAll(cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) error {
		_, err := compref.Parse(args[0])
		return err
	}),
	Long: fmt.Sprintf(`Get component version(s) from an OCM repository.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

For valid prefixes {%[1]s|none} are available. If <none> is used, it defaults to %[1]q. This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {%[2]s} are supported, which can be shortened to {%[3]s} respectively for convenience.

If no type is given, the repository path is interpreted based on introspection and heuristics.
`,
		compref.DefaultPrefix,
		strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
		strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
	),
	Example: strings.TrimSpace(`
Getting a single component version:

get component-version ghcr.com/open-component-model/ocm//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf/component-descriptors/ocm.software/ocmcli:0.23.0

Listing many component versions:

get component-versions ghcr.com/open-component-model/ocm//ocm.software/ocmcli
get cvs ghcr.com/open-component-model/ocm//ocm.software/ocmcli --output json
get cvs ghcr.com/open-component-model/ocm//ocm.software/ocmcli -oyaml

Specifying types and schemes:

get cv ctf::github.com/locally-checked-out-repo//ocm.software/ocmcli:0.23.0
get cvs oci::http://localhost:8080//ocm.software/ocmcli
`),
	Version:           "v1alpha1",
	RunE:              getComponentVersion,
	DisableAutoGenTag: true,
}

func init() {
	enum.VarP(GetComponentVersionCmd.Flags(), "output", "o", []string{"table", "yaml", "json"}, "output format of the component descriptors")
	GetComponentVersionCmd.Flags().String("semver-constraint", "> 0.0.0-0", "semantic version constraint restricting which versions to output")
	GetComponentVersionCmd.Flags().Int("concurrency-limit", goruntime.NumCPU(), "maximum amount of parallel requests to the repository for resolving component versions")
	GetCmd.AddCommand(GetComponentVersionCmd)
}

func getComponentVersion(cmd *cobra.Command, args []string) error {
	output, err := enum.Get(cmd.Flags(), "output")
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}
	constraint, err := cmd.Flags().GetString("semver-constraint")
	if err != nil {
		return fmt.Errorf("getting semver-constraint flag failed: %w", err)
	}
	concurrencyLimit, err := cmd.Flags().GetInt("concurrency-limit")
	if err != nil {
		return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	}

	reference := args[0]
	repo, err := ocm.New(cmd.Context(), Root.PluginManager, Root.CredentialGraph, reference)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	descs, err := repo.GetComponentVersions(cmd.Context(), ocm.GetComponentVersionsOptions{
		VersionOptions: ocm.VersionOptions{
			SemverConstraint: constraint,
		},
		ConcurrencyLimit: concurrencyLimit,
	})
	if err != nil {
		return fmt.Errorf("getting component reference and versions failed: %w", err)
	}

	reader, size, err := encodeDescriptors(output, descs)
	if err != nil {
		return fmt.Errorf("generating output failed: %w", err)
	}

	if _, err := io.CopyN(cmd.OutOrStdout(), reader, size); err != nil {
		return fmt.Errorf("writing component version descriptor failed: %w", err)
	}

	return nil
}

func encodeDescriptors(output string, descs []*descruntime.Descriptor) (io.Reader, int64, error) {
	var data []byte
	var err error
	switch output {
	case "json":
		data, err = encodeDescriptorsAsNDJSON(descs)
	case "yaml":
		data, err = encodeDescriptorsAsYAML(descs)
	case "table":
		data, err = encodeDescriptorsAsTable(descs)
	default:
		err = fmt.Errorf("unknown output format: %q", output)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("encoding component version descriptor as %q failed: %w", output, err)
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

func encodeDescriptorsAsNDJSON(descs []*descruntime.Descriptor) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, desc := range descs {
		// TODO(add formatting options for scheme version with v2 as only option)
		v2descriptor, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
		if err != nil {
			return nil, fmt.Errorf("converting component version to v2 descriptor failed: %w", err)
		}
		// TODO(add formatting options for yaml/json)
		// multiple output is equivalent to NDJSON (new line delimited json), may want array access
		if err := encoder.Encode(v2descriptor); err != nil {
			return nil, fmt.Errorf("encoding component version descriptor failed: %w", err)
		}
	}
	return buf.Bytes(), nil
}

func encodeDescriptorsAsYAML(descriptor []*descruntime.Descriptor) ([]byte, error) {
	// TODO(add formatting options for scheme version with v2 as only option)
	v2List := make([]*v2.Descriptor, len(descriptor))
	for i, desc := range descriptor {
		v2descriptor, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
		if err != nil {
			return nil, fmt.Errorf("converting component version to v2 descriptor failed: %w", err)
		}
		v2List[i] = v2descriptor
	}

	if len(v2List) == 1 {
		return yaml.Marshal(v2List[0])
	}

	return yaml.Marshal(v2List)
}

func encodeDescriptorsAsTable(descriptor []*descruntime.Descriptor) ([]byte, error) {
	var buf bytes.Buffer
	t := table.NewWriter()
	t.SetOutputMirror(&buf)
	t.AppendHeader(table.Row{"Component", "Version", "Provider"})
	for _, desc := range descriptor {
		t.AppendRow(table.Row{desc.Component.Name, desc.Component.Version, desc.Component.Provider.String()})
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 3, AutoMerge: true},
	})
	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return buf.Bytes(), nil
}
