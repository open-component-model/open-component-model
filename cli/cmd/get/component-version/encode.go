package componentversion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"sigs.k8s.io/yaml"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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
		// TODO(jakobmoellerdev): add formatting options for scheme version with v2 as only option
		v2descriptor, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
		if err != nil {
			return nil, fmt.Errorf("converting component version to v2 descriptor failed: %w", err)
		}
		// TODO(jakobmoellerdev): add formatting options for yaml/json
		// multiple output is equivalent to NDJSON (new line delimited json), may want array access
		if err := encoder.Encode(v2descriptor); err != nil {
			return nil, fmt.Errorf("encoding component version descriptor failed: %w", err)
		}
	}
	return buf.Bytes(), nil
}

func encodeDescriptorsAsYAML(descriptor []*descruntime.Descriptor) ([]byte, error) {
	// TODO(jakobmoellerdev): add formatting options for scheme version with v2 as only option
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
