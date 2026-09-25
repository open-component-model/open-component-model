package configuration

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/spf13/cobra"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SkipStdinConfigAnnotation marks a command that never uses configuration, together with
// its subcommands. Piped stdin is not read for it: reading waits until stdin is closed,
// which would hang the command in a shell or CI job that keeps stdin open.
const SkipStdinConfigAnnotation = "ocm.software/skip-stdin-config"

// builtinCommands are the commands cobra adds itself. They cannot carry
// SkipStdinConfigAnnotation, so they are matched by name.
var builtinCommands = []string{"help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd}

var documentSeparator = []byte("---\n")

// AddStdinConfig applies the configuration documents found in piped stdin on top of cfg,
// so configuration such as credentials can be passed without writing a file.
//
// Stdin can be read only once, but the command may read it too (for example
// --transfer-spec -). So the configuration is taken out here, before the command runs,
// and every other document is put back for the command. If stdin holds no configuration
// or is not valid YAML, it is put back unchanged. A terminal is never read.
func AddStdinConfig(cmd *cobra.Command, cfg *genericv1.Config) (*genericv1.Config, error) {
	in := cmd.InOrStdin()
	if skipsStdinConfig(cmd) || !isPiped(in) {
		return cfg, nil
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	configs, others, err := SplitConfigStream(bytes.NewReader(data))
	if err != nil || len(configs) == 0 {
		cmd.SetIn(bytes.NewReader(data))
		return cfg, nil
	}
	cmd.SetIn(bytes.NewReader(bytes.Join(others, documentSeparator)))

	cfgs := []*genericv1.Config{cfg}
	for _, doc := range configs {
		stdinCfg, err := decodeConfig(bytes.NewReader(doc))
		if err != nil {
			return nil, fmt.Errorf("could not load configuration from stdin: %w", err)
		}
		cfgs = append(cfgs, stdinCfg)
	}
	return genericv1.FlatMap(cfgs...), nil
}

// skipsStdinConfig reports whether cmd or one of its parents never uses configuration.
func skipsStdinConfig(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if _, ok := c.Annotations[SkipStdinConfigAnnotation]; ok {
			return true
		}
		if c.HasParent() && slices.Contains(builtinCommands, c.Name()) {
			return true
		}
	}
	return false
}

// isPiped reports whether r is piped input and not a terminal. A reader that is not a
// file, as set with cmd.SetIn, counts as piped.
func isPiped(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
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
