package internal

import "testing"

func TestSanitizeContainerName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already valid",
			input:    "my-container.name_1",
			expected: "my-container.name_1",
		},
		{
			name:     "subtest with slash",
			input:    "test_integration_addcomponentversion_ocirepository/targeting_defaults",
			expected: "test_integration_addcomponentversion_ocirepository-targeting_defaults",
		},
		{
			name:     "uppercase letters",
			input:    "TestFoo/Bar",
			expected: "testfoo-bar",
		},
		{
			name:     "multiple invalid characters",
			input:    "test/sub test:v1",
			expected: "test-sub-test-v1",
		},
		{
			name:     "simple name",
			input:    "simple",
			expected: "simple",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeContainerName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeContainerName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
