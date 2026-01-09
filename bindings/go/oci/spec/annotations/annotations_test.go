package annotations

import (
	"fmt"
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
			component: "ocm.software/test-component",
			version:   "1.0.0",
			expected:  "ocm.software/test-component:1.0.0",
		},
		{
			name:      "empty component",
			component: "",
			version:   "1.0.0",
			expected:  ":1.0.0",
		},
		{
			name:      "empty version",
			component: "ocm.software/test-component",
			version:   "",
			expected:  "ocm.software/test-component:",
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
			name:         "valid annotation (prefixed)",
			annotation:   "component-descriptors/ocm.software/test-component:1.0.0",
			expectedComp: "ocm.software/test-component",
			expectedVer:  "1.0.0",
		},
		{
			name:         "valid annotation",
			annotation:   "ocm.software/test-component:1.0.0",
			expectedComp: "ocm.software/test-component",
			expectedVer:  "1.0.0",
		},
		{
			name:         "valid annotation (name without prefix but with slash)",
			annotation:   "ocm.software/abc/def/test-component:1.0.0",
			expectedComp: "ocm.software/abc/def/test-component",
			expectedVer:  "1.0.0",
		},
		{
			name:         "valid annotation (name without prefix but with slash and prefix)",
			annotation:   "component-descriptors/ocm.software/abc/def/test-component:1.0.0",
			expectedComp: "ocm.software/abc/def/test-component",
			expectedVer:  "1.0.0",
		},
		{
			name:          "invalid format - missing colon",
			annotation:    "component-descriptors/ocm.software/test-component",
			expectedError: fmt.Sprintf("%q is not considered a valid %q annotation, not exactly 2 parts: [%[1]q]", "ocm.software/test-component", OCMComponentVersion),
		},
		{
			name:          "invalid format - empty version",
			annotation:    "component-descriptors/test-component:",
			expectedError: "version parsed from \"test-component:\" in \"software.ocm.componentversion\" annotation is empty but should not be",
		},
		{
			name:       "invalid format - multiple separators",
			annotation: "wrong/test-component:1.0.0:1.0.0",
			expectedError: fmt.Sprintf("%q is not considered a valid %q annotation, not exactly 2 parts: %q",
				"wrong/test-component:1.0.0:1.0.0",
				OCMComponentVersion,
				[]string{"wrong/test-component", "1.0.0", "1.0.0"},
			),
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
