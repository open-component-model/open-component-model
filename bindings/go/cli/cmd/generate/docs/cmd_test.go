package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestConvertMarkdownLinksToRelref(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple relative link",
			input: `[foo](bar.md)`,
			want:  `[foo]({{< relref "bar.md" >}})`,
		},
		{
			name:  "path stripped to basename",
			input: `[foo](subdir/bar.md)`,
			want:  `[foo]({{< relref "bar.md" >}})`,
		},
		{
			name:  "deep path stripped to basename",
			input: `[foo](a/b/c/bar.md)`,
			want:  `[foo]({{< relref "bar.md" >}})`,
		},
		{
			name:  "external http link untouched",
			input: `[ext](http://example.com/page.md)`,
			want:  `[ext](http://example.com/page.md)`,
		},
		{
			name:  "external https link untouched",
			input: `[ext](https://example.com/page.md)`,
			want:  `[ext](https://example.com/page.md)`,
		},
		{
			name:  "multiple links on one line",
			input: `See [a](a.md) and [b](dir/b.md) for more.`,
			want:  `See [a]({{< relref "a.md" >}}) and [b]({{< relref "b.md" >}}) for more.`,
		},
		{
			name:  "mixed internal and external links",
			input: `See [a](a.md) and [ext](https://example.com/x.md).`,
			want:  `See [a]({{< relref "a.md" >}}) and [ext](https://example.com/x.md).`,
		},
		{
			name:  "no links at all",
			input: `Just some text without links.`,
			want:  `Just some text without links.`,
		},
		{
			name:  "link text with spaces",
			input: `[some command](some_command.md)`,
			want:  `[some command]({{< relref "some_command.md" >}})`,
		},
		{
			name:  "anchor links are not .md so remain unchanged",
			input: `[section](#heading)`,
			want:  `[section](#heading)`,
		},
		{
			name:  "link to non-md file unchanged",
			input: `[readme](README.txt)`,
			want:  `[readme](README.txt)`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r = require.New(t)
			got := convertMarkdownLinksToRelref(tc.input)
			r.Equal(tc.want, got)
		})
	}
}

// newTestCobraTree builds a small synthetic cobra command tree for testing doc generation.
// root -> sub1 (visible), sub2 (hidden), sub1 -> leaf
func newTestCobraTree() *cobra.Command {
	root := &cobra.Command{
		Use:   "myapp",
		Short: "My test application",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	sub1 := &cobra.Command{
		Use:   "sub1",
		Short: "First sub-command",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	leaf := &cobra.Command{
		Use:   "leaf",
		Short: "A leaf command",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	sub1.AddCommand(leaf)
	sub2 := &cobra.Command{
		Use:    "hidden",
		Short:  "A hidden command",
		Hidden: true,
		RunE:   func(_ *cobra.Command, _ []string) error { return nil },
	}
	root.AddCommand(sub1, sub2)
	return root
}

func TestGenerateHugoMarkdown_EndToEnd(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	root := newTestCobraTree()

	err := generateHugoMarkdown(root, dir)
	r.NoError(err)

	// There should be one file per visible command: root, sub1, leaf = 3 files + _index.md = 4
	entries, err := os.ReadDir(dir)
	r.NoError(err)

	fileNames := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		fileNames[e.Name()] = struct{}{}
	}

	// Expected files: myapp.md, myapp_sub1.md, myapp_sub1_leaf.md, _index.md
	expectedFiles := []string{"myapp.md", "myapp_sub1.md", "myapp_sub1_leaf.md", "_index.md"}
	for _, name := range expectedFiles {
		_, ok := fileNames[name]
		r.True(ok, "expected file %s to exist", name)
	}

	// Hidden command must NOT have a file
	_, hasHidden := fileNames["myapp_hidden.md"]
	r.False(hasHidden, "hidden command should not produce a doc file")

	// Total files should be exactly expected (no extras)
	r.Len(entries, len(expectedFiles))
}

func TestGenerateHugoMarkdown_FrontMatter(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	root := newTestCobraTree()

	r.NoError(generateHugoMarkdown(root, dir))

	// Check root command file has correct frontmatter
	data, err := os.ReadFile(filepath.Join(dir, "myapp.md"))
	r.NoError(err)
	content := string(data)

	r.True(strings.HasPrefix(content, "---\n"), "file should start with YAML frontmatter delimiter")
	r.Contains(content, "title: myapp")
	r.Contains(content, "description: My test application.")
	r.Contains(content, "suppressTitle: true")
	r.Contains(content, "toc: true")
	r.Contains(content, "sidebar:")
	r.Contains(content, "collapsed: true")

	// After frontmatter there should be markdown content
	parts := strings.SplitN(content, "---\n", 3)
	r.Len(parts, 3, "expected two --- delimiters producing three parts")
	body := parts[2]
	r.NotEmpty(strings.TrimSpace(body), "markdown body should not be empty")
}

func TestGenerateHugoMarkdown_IndexFile(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	root := newTestCobraTree()
	r.NoError(generateHugoMarkdown(root, dir))

	data, err := os.ReadFile(filepath.Join(dir, "_index.md"))
	r.NoError(err)
	content := string(data)

	r.Contains(content, "title: OCM CLI")
	r.Contains(content, "description: Reference Documentation for the OCM CLI.")
	r.Contains(content, "suppressTitle: false")
}

func TestGenerateHugoMarkdown_RelrefInOutput(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	root := newTestCobraTree()
	r.NoError(generateHugoMarkdown(root, dir))

	// sub1 command doc should contain a relref link to the leaf sub-command doc
	data, err := os.ReadFile(filepath.Join(dir, "myapp_sub1.md"))
	r.NoError(err)
	content := string(data)

	// The cobra markdown generator produces a SEE ALSO section with links like [myapp sub1 leaf](myapp_sub1_leaf.md)
	// which our converter turns into relref links
	r.Contains(content, `relref "myapp_sub1_leaf.md"`, "sub-command links should be converted to relref")
}

func TestGenerateHugoMarkdown_DescriptionEndingWithPeriod(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	// A command whose Short already ends with a period
	root := &cobra.Command{
		Use:   "dotapp",
		Short: "Already has a period.",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	r.NoError(generateHugoMarkdown(root, dir))

	data, err := os.ReadFile(filepath.Join(dir, "dotapp.md"))
	r.NoError(err)
	content := string(data)

	// Should NOT double-period
	r.Contains(content, "description: Already has a period.")
	r.NotContains(content, "description: Already has a period..")
}

func TestGenerateHugoMarkdown_EmptyShortDescription(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	root := &cobra.Command{
		Use:  "noshort",
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}
	r.NoError(generateHugoMarkdown(root, dir))

	data, err := os.ReadFile(filepath.Join(dir, "noshort.md"))
	r.NoError(err)
	content := string(data)

	// Fallback description should be generated
	r.Contains(content, "description: Documentation for the noshort command.")
}

func TestCreateHugoIndexFile(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	r.NoError(createHugoIndexFile(dir))

	data, err := os.ReadFile(filepath.Join(dir, "_index.md"))
	r.NoError(err)
	content := string(data)

	r.Contains(content, "title: OCM CLI")
	r.Contains(content, "description: Reference Documentation for the OCM CLI.")
	r.True(strings.HasPrefix(content, "---\n"))
	r.True(strings.HasSuffix(content, "---\n"))
}

func TestGenerateHugoMarkdownTree_SkipsUnavailableCommands(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	root := &cobra.Command{
		Use:   "app",
		Short: "Root",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	// A normal visible sub-command should appear
	visible := &cobra.Command{
		Use:   "visible",
		Short: "Visible cmd",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	// A hidden command should be skipped (IsAvailableCommand returns false)
	hidden := &cobra.Command{
		Use:    "secret",
		Short:  "Secret cmd",
		Hidden: true,
		RunE:   func(_ *cobra.Command, _ []string) error { return nil },
	}
	// A deprecated command is also skipped by cobra's IsAvailableCommand
	deprecated := &cobra.Command{
		Use:        "old",
		Short:      "Deprecated cmd",
		Deprecated: "use new instead",
		RunE:       func(_ *cobra.Command, _ []string) error { return nil },
	}
	root.AddCommand(visible, hidden, deprecated)

	gen := &hugoGenerator{dir: dir}
	r.NoError(generateHugoMarkdownTree(root, gen))

	entries, err := os.ReadDir(dir)
	r.NoError(err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	r.Contains(names, "app.md")
	r.Contains(names, "app_visible.md", "visible commands should appear")
	r.NotContains(names, "app_secret.md", "hidden commands should be excluded")
	r.NotContains(names, "app_old.md", "deprecated commands should be excluded (IsAvailableCommand returns false)")
}
