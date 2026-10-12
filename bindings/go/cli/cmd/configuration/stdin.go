package configuration

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// StdinConfigPath is the --config entry that stands for stdin.
const StdinConfigPath = "-"

// readStdinConfigs returns the configuration documents piped into stdin, in stream order.
// Stdin is read only on an explicit --config -: an unconditional read waits until stdin
// is closed, which hangs the command in a shell or CI job that keeps stdin open.
//
// Stdin can be read only once, but the command may read it too (for example
// --transfer-spec -). So the configuration is taken out here, before the command runs,
// and every other document is put back for the command.
func readStdinConfigs(cmd *cobra.Command) ([]*genericv1.Config, error) {
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	configs, others, err := SplitConfigStream(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}
	if len(configs) == 0 {
		return nil, errors.New("stdin: no configuration document found")
	}
	cmd.SetIn(bytes.NewReader(bytes.Join(others, []byte("---\n"))))

	cfgs := make([]*genericv1.Config, 0, len(configs))
	for _, doc := range configs {
		cfg, err := decodeConfig(bytes.NewReader(doc))
		if err != nil {
			return nil, fmt.Errorf("stdin: %w", err)
		}
		cfgs = append(cfgs, cfg)
	}
	return cfgs, nil
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
