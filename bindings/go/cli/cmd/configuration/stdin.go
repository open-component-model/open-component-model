package configuration

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// StdinFlagAnnotation marks a command flag that reads stdin when set to "-". Set it with
// cmd.Flags().SetAnnotation so AddStdinConfig picks up configuration documents from stdin.
const StdinFlagAnnotation = "ocm.software/reads-stdin"

var documentSeparator = []byte("---\n")

// readConfigStream reads r as a YAML stream and returns the merged configuration together
// with the documents that were not configuration, joined again with "---".
//
// Stdin can be read only once, but --config - and a command flag such as --transfer-spec -
// may both point at it. The configuration is loaded first, so this is the one place that
// reads the stream; the caller hands the rest back to the command.
//
// A stream without a configuration document is an error: a caller who passes "-" expects
// to supply one, and a silent empty config would hide a broken pipe.
func readConfigStream(r io.Reader) (cfg *genericv1.Config, rest []byte, err error) {
	configs, others, err := SplitConfigStream(r)
	if err != nil {
		return nil, nil, err
	}
	if len(configs) == 0 {
		if len(others) == 0 {
			return nil, nil, errors.New("no data was read")
		}
		return nil, nil, fmt.Errorf("no configuration document of type %q was read", genericv1.ConfigType)
	}
	cfg, err = decodeConfigs(configs)
	if err != nil {
		return nil, nil, err
	}
	return cfg, bytes.Join(others, documentSeparator), nil
}

// AddStdinConfig applies the configuration documents found in stdin on top of cfg, when a
// flag marked with StdinFlagAnnotation is "-" and --config does not read stdin itself.
// A user who pipes configuration and data together should not have to also pass
// --config -. The other documents are put back on stdin for the command. In every other
// case cfg is returned unchanged and stdin is not read.
func AddStdinConfig(cmd *cobra.Command, cfg *genericv1.Config) (*genericv1.Config, error) {
	if !flagReadsStdin(cmd) || configFlagReadsStdin(cmd) {
		return cfg, nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	configs, others, err := SplitConfigStream(bytes.NewReader(data))
	if err != nil {
		// Not valid YAML: hand stdin back unchanged, so the command reports the error in
		// terms of its own input.
		cmd.SetIn(bytes.NewReader(data))
		return cfg, nil
	}
	cmd.SetIn(bytes.NewReader(bytes.Join(others, documentSeparator)))
	if len(configs) == 0 {
		return cfg, nil
	}
	stdinCfg, err := decodeConfigs(configs)
	if err != nil {
		return nil, fmt.Errorf("could not load configuration from stdin: %w", err)
	}
	return genericv1.FlatMap(cfg, stdinCfg), nil
}

// flagReadsStdin reports whether a flag marked with StdinFlagAnnotation is set to "-".
func flagReadsStdin(cmd *cobra.Command) bool {
	found := false
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if _, ok := f.Annotations[StdinFlagAnnotation]; ok && f.Value.String() == StdinConfigPath {
			found = true
		}
	})
	return found
}

func configFlagReadsStdin(cmd *cobra.Command) bool {
	flag := cmd.Flag(OCMConfigCommandArgument)
	if flag == nil || !flag.Changed {
		return false
	}
	return slices.Contains(flag.Value.(pflag.SliceValue).GetSlice(), StdinConfigPath)
}

func decodeConfigs(docs [][]byte) (*genericv1.Config, error) {
	cfgs := make([]*genericv1.Config, 0, len(docs))
	for _, doc := range docs {
		cfg, err := decodeConfig(bytes.NewReader(doc))
		if err != nil {
			return nil, err
		}
		cfgs = append(cfgs, cfg)
	}
	return genericv1.FlatMap(cfgs...), nil
}

// SplitConfigStream splits a YAML stream on "---" into the documents typed as OCM
// configuration and all others, both in stream order. Empty documents are dropped.
// A document that is not valid YAML fails the whole stream: no consumer could use it,
// and the parse error names the problem.
func SplitConfigStream(r io.Reader) (configs, others [][]byte, err error) {
	reader := k8syaml.NewYAMLReader(bufio.NewReader(r))
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return configs, others, nil
		}
		if err != nil {
			return nil, nil, err
		}
		doc = trimLeadingSeparator(doc)
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		isConfig, err := isConfigDocument(doc)
		if err != nil {
			return nil, nil, err
		}
		if isConfig {
			configs = append(configs, doc)
		} else {
			others = append(others, doc)
		}
	}
}

// trimLeadingSeparator drops a "---" line at the start of a document. The reader keeps
// that line when the document follows an empty one, which would make an empty document
// look like content.
func trimLeadingSeparator(doc []byte) []byte {
	if !bytes.HasPrefix(doc, []byte("---")) {
		return doc
	}
	i := bytes.IndexByte(doc, '\n')
	if i < 0 {
		return nil
	}
	return doc[i+1:]
}

// isConfigDocument reports whether the document declares the generic configuration type,
// in any version. Only the type is inspected, so nested types (for example inside a
// transfer spec) do not count.
func isConfigDocument(doc []byte) (bool, error) {
	var header struct {
		Type runtime.Type `json:"type"`
	}
	if err := yaml.Unmarshal(doc, &header); err != nil {
		return false, err
	}
	return header.Type.Name == genericv1.ConfigType, nil
}
