package annotations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewComponentVersionAnnotation(t *testing.T) {
	tests := []struct {
		name      string
		component string
		version   string
		expected  string
	}{
		{
			name:      "valid component and version",
			component: "test-component",
			version:   "1.0.0",
			expected:  "component-descriptors/test-component:1.0.0",
		},
		{
			name:      "empty component",
			component: "",
			version:   "1.0.0",
			expected:  "component-descriptors/:1.0.0",
		},
		{
			name:      "empty version",
			component: "test-component",
			version:   "",
			expected:  "component-descriptors/test-component:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewComponentVersionAnnotation(tt.component, tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseComponentVersionAnnotation(t *testing.T) {
	tests := []struct {
		name          string
		annotation    string
		expectedComp  string
		expectedVer   string
		expectedError string
	}{
		{
			name:         "valid annotation",
			annotation:   "component-descriptors/test-component:1.0.0",
			expectedComp: "test-component",
			expectedVer:  "1.0.0",
		},
		{
			name:          "invalid format - missing colon",
			annotation:    "component-descriptors/test-component",
			expectedError: "\"component-descriptors/test-component\" is not considered a valid \"software.ocm.componentversion\" annotation",
		},
		{
			name:          "invalid format - empty version",
			annotation:    "component-descriptors/test-component:",
			expectedError: "version parsed from \"component-descriptors/test-component:\" in \"software.ocm.componentversion\" annotation is empty but should not be",
		},
		{
			name:          "invalid format - wrong prefix",
			annotation:    "wrong-prefix/test-component:1.0.0",
			expectedError: "\"wrong-prefix/test-component:1.0.0\" is not considered a valid \"software.ocm.componentversion\" annotation because of a bad prefix, expected \"component-descriptors/\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp, ver, err := ParseComponentVersionAnnotation(tt.annotation)
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Equal(t, tt.expectedError, err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedComp, comp)
			assert.Equal(t, tt.expectedVer, ver)
		})
	}
}
