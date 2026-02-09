package types_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd/internal/test"
)

func TestDescribeTypesListSubsystems(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()

	// Should contain known subsystems
	assert.Contains(t, output, "input")
	assert.Contains(t, output, "ocm-repository")
	assert.Contains(t, output, "signing")
}

func TestDescribeTypesListTypes(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()

	// Should contain known input types
	assert.Contains(t, output, "file")
	assert.Contains(t, output, "dir")
}

func TestDescribeTypesDescribeType(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "file/v1"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()

	// Should contain field information
	assert.Contains(t, output, "path")
}

func TestDescribeTypesFieldPathNavigation(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	// Navigate into a field - even leaf fields can be navigated to
	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "file/v1", "path"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()

	// Should show breadcrumb with navigation path
	assert.Contains(t, output, "file/v1")
	assert.Contains(t, output, "path")
}

func TestDescribeTypesInvalidPath(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "file/v1", "nonexistent"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.Error(t, err)

	// Error should mention "not found" and list available fields
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "path")
}

func TestDescribeTypesShowPaths(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "file/v1", "--show-paths"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()

	// Should show available field paths
	assert.Contains(t, output, "path")
	// Table headers are uppercase
	assert.Contains(t, output, "DEPTH")
}

func TestDescribeTypesUnknownSubsystem(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "nonexistent-subsystem"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown subsystem")
}

func TestDescribeTypesUnknownType(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "nonexistent/v999"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestDescribeTypesJSONSchemaOutput(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "file/v1", "-o", "jsonschema"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	// Should be valid JSON
	var schema map[string]any
	err = json.Unmarshal(result.Bytes(), &schema)
	require.NoError(t, err, "output should be valid JSON schema")

	// Should have typical JSON Schema fields
	assert.Contains(t, schema, "type")
	assert.Contains(t, schema, "properties")
}

func TestDescribeTypesMarkdownOutput(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "file/v1", "-o", "markdown"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()

	// Markdown tables have | separators
	assert.True(t, strings.Contains(output, "|"))
}

func TestDescribeTypesHTMLOutput(t *testing.T) {
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "types", "input", "file/v1", "-o", "html"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()

	// HTML output should have table tags
	assert.Contains(t, output, "<table")
	assert.Contains(t, output, "</table>")
}

func TestDescribeTypesTypeAlias(t *testing.T) {
	// Test that "type" alias works same as "types"
	result := new(bytes.Buffer)
	logs := test.NewJSONLogReader()

	_, err := test.OCM(t,
		test.WithArgs("describe", "type", "input"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	require.NoError(t, err)

	output := result.String()
	assert.Contains(t, output, "file")
}
