package component_version

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// uploaderUsage is the --uploader help text, listing the registered uploader names.
func uploaderUsage() string {
	return `add an uploader configuration entry after those from the OCM configuration (repeatable, in the given order): an uploader name (` +
		strings.Join(transferv1alpha1.UploaderNames(), ", ") +
		`), <name>=<CEL match expression> (e.g. 'reference=resource.name == "my-image"'), or a YAML/JSON mapping of the entry whose "type" is a name. "localblob" copies every resource no other uploader selects`
}

// uploaderEntry turns one --uploader value into a central config entry:
//   - "<name>"         an entry with only a type (the type's default match),
//   - "<name>=<CEL>"   an entry with that match expression,
//   - "{...}"          a YAML/JSON mapping of the whole entry whose "type" is a name.
//
// <name> is a short uploader name or full type (transferv1alpha1.ResolveUploaderType).
// Fields are not validated here: LookupUploaderConfigs decodes strictly and validates,
// so flag and file entries fail identically.
func uploaderEntry(value string) (*runtime.Raw, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil, fmt.Errorf("empty value")
	}

	var entry map[string]any
	if strings.HasPrefix(v, "{") {
		data, err := yaml.YAMLToJSON([]byte(v))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, err
		}
		name, ok := entry["type"].(string)
		if !ok {
			return nil, fmt.Errorf(`missing "type"`)
		}
		t, err := transferv1alpha1.ResolveUploaderType(name)
		if err != nil {
			return nil, err
		}
		entry["type"] = t.String()
		return rawEntry(t, entry)
	}

	name, expr, hasMatch := strings.Cut(v, "=")
	t, err := transferv1alpha1.ResolveUploaderType(strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	entry = map[string]any{"type": t.String()}
	if hasMatch {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			return nil, fmt.Errorf("empty match")
		}
		entry["match"] = expr
	}
	return rawEntry(t, entry)
}

func rawEntry(t runtime.Type, entry map[string]any) (*runtime.Raw, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	return &runtime.Raw{Type: t, Data: data}, nil
}

// withFlagUploaders returns a copy of cfg whose configurations are cfg's entries followed
// by the --uploader entries in flag order, then the entries translated from the deprecated
// --copy-resources and --upload-as flags (last, like the catch-all --copy-resources used to
// append). cfg is not modified; a nil cfg counts as empty.
func withFlagUploaders(cmd *cobra.Command, cfg *genericv1.Config) (*genericv1.Config, error) {
	// GetStringArray round-trips through the flag's string form, which turns a single
	// empty value into no values; read the slice directly so it is reported instead.
	var values []string
	if f := cmd.Flags().Lookup(FlagUploader); f != nil {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			values = sv.GetSlice()
		}
	}
	legacy, err := legacyUploaderValuesFromFlags(cmd)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 && len(legacy) == 0 {
		return cfg, nil
	}
	entries := make([]*runtime.Raw, 0, len(values)+len(legacy))
	for _, v := range values {
		entry, err := uploaderEntry(v)
		if err != nil {
			return nil, fmt.Errorf("invalid --%s %q: %w", FlagUploader, v, err)
		}
		entries = append(entries, entry)
	}
	for _, v := range legacy {
		entry, err := uploaderEntry(v)
		if err != nil {
			return nil, fmt.Errorf("translating --%s/--%s to uploader %q: %w", FlagCopyResources, FlagUploadAs, v, err)
		}
		entries = append(entries, entry)
	}
	out := &genericv1.Config{}
	if cfg != nil {
		out.Type = cfg.Type
		out.Configurations = slices.Clone(cfg.Configurations)
	}
	out.Configurations = append(out.Configurations, entries...)
	return out, nil
}
