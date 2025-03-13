package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/golang/runtime"
)

func TestTypeFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected runtime.Type
		wantErr  bool
	}{
		// Unversioned types
		{"OCIArtifact", runtime.Type{Name: "OCIArtifact"}, false},
		{"software.ocm.accessType.OCIArtifact", runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact"}, false},

		// Versioned types
		{"OCIArtifact/v1", runtime.Type{Name: "OCIArtifact", Version: "v1"}, false},
		{"software.ocm.accessType.OCIArtifact/v1", runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact", Version: "v1"}, false},

		// Invalid formats
		{"", runtime.Type{}, true},
		{"software.ocm.accessType./v1", runtime.Type{}, true},
		{"./v1", runtime.Type{}, true},
		{"software.ocm.accessType.OCIArtifact/v1/extra", runtime.Type{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := runtime.TypeFromString(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestTypeString(t *testing.T) {
	tests := []struct {
		typ      runtime.Type
		expected string
	}{
		{runtime.Type{Name: "OCIArtifact"}, "OCIArtifact"},
		{runtime.Type{Name: "OCIArtifact", Version: "v1"}, "OCIArtifact/v1"},
		{runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact"}, "software.ocm.accessType.OCIArtifact"},
		{runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact", Version: "v1"}, "software.ocm.accessType.OCIArtifact/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.typ.String())
		})
	}
}

func TestTypeEqual(t *testing.T) {
	tests := []struct {
		type1   runtime.Type
		type2   runtime.Type
		isEqual bool
	}{
		{runtime.Type{Name: "OCIArtifact"}, runtime.Type{Name: "OCIArtifact"}, true},
		{runtime.Type{Name: "OCIArtifact", Version: "v1"}, runtime.Type{Name: "OCIArtifact", Version: "v1"}, true},
		{runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact"}, runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact"}, true},
		{runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact", Version: "v1"}, runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact", Version: "v1"}, true},

		// Different cases
		{runtime.Type{Name: "OCIArtifact"}, runtime.Type{Name: "Node"}, false},
		{runtime.Type{Name: "OCIArtifact", Version: "v1"}, runtime.Type{Name: "OCIArtifact", Version: "v2"}, false},
		{runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact"}, runtime.Type{Group: "apps", Name: "OCIArtifact"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.type1.String()+"_vs_"+tt.type2.String(), func(t *testing.T) {
			assert.Equal(t, tt.isEqual, tt.type1.Equal(tt.type2))
		})
	}
}

func TestJSONMarshalling(t *testing.T) {
	tests := []struct {
		typ      runtime.Type
		expected string
	}{
		{runtime.Type{Name: "OCIArtifact"}, `"OCIArtifact"`},
		{runtime.Type{Name: "OCIArtifact", Version: "v1"}, `"OCIArtifact/v1"`},
		{runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact"}, `"software.ocm.accessType.OCIArtifact"`},
		{runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact", Version: "v1"}, `"software.ocm.accessType.OCIArtifact/v1"`},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			data, err := json.Marshal(tt.typ)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

func TestJSONUnmarshalling(t *testing.T) {
	tests := []struct {
		jsonStr  string
		expected runtime.Type
		wantErr  bool
	}{
		{`"OCIArtifact"`, runtime.Type{Name: "OCIArtifact"}, false},
		{`"OCIArtifact/v1"`, runtime.Type{Name: "OCIArtifact", Version: "v1"}, false},
		{`"software.ocm.accessType.OCIArtifact"`, runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact"}, false},
		{`"software.ocm.accessType.OCIArtifact/v1"`, runtime.Type{Group: "software.ocm.accessType", Name: "OCIArtifact", Version: "v1"}, false},

		// Invalid JSON cases
		{`""`, runtime.Type{}, true},
		{`"software.ocm.accessType./v1"`, runtime.Type{}, true},
		{`"./v1"`, runtime.Type{}, true},
		{`"software.ocm.accessType.OCIArtifact/v1/extra"`, runtime.Type{}, true},
		{`123`, runtime.Type{}, true}, // Not a string
	}

	for _, tt := range tests {
		t.Run(tt.jsonStr, func(t *testing.T) {
			var result runtime.Type
			err := json.Unmarshal([]byte(tt.jsonStr), &result)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
