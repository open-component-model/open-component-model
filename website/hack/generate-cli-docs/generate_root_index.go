package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Builds the root help index page for additional topics.
func genIndexForRootHelpTopics(dir string) (err error) {
	var topics []string
	if err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if !info.IsDir() {
			topics = append(topics, strings.TrimSuffix(filepath.Base(path), ".md"))
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to gather help topics: %w", err)
	}

	filename := filepath.Join(dir, "_index.md")

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create index file %q: %v", filename, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	f.WriteString(fmt.Sprintf(fmTmpl, "help", "help", "docs/reference/ocm-cli/help/"))
	f.WriteString("### Additional Topics\n\n")

	for _, topic := range topics {
		relref := toRelref(fmt.Sprintf("docs/reference/ocm-cli/help/%s.md", topic))
		f.WriteString(fmt.Sprintf("* [%s](%s) &mdash; %s\n", topic, relref, topic))
	}

	return nil
}
