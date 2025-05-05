package ctf

import (
	"testing"

	"ocm.software/open-component-model/bindings/go/ctf"
)

func TestRepository_String(t *testing.T) {
	tests := []struct {
		name     string
		repo     Repository
		expected string
	}{
		{
			name: "relative path",
			repo: Repository{
				Path: "./test/archive.tgz",
			},
			expected: "./test/archive.tgz",
		},
		{
			name: "absolute path",
			repo: Repository{
				Path: "/absolute/path/to/archive",
			},
			expected: "/absolute/path/to/archive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.repo.String(); got != tt.expected {
				t.Errorf("Repository.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAccessMode_ToAccessBitmask(t *testing.T) {
	tests := []struct {
		name     string
		mode     AccessMode
		expected int
	}{
		{
			name:     "readonly",
			mode:     AccessModeReadOnly,
			expected: ctf.O_RDONLY,
		},
		{
			name:     "readwrite",
			mode:     AccessModeReadWrite,
			expected: ctf.O_RDWR,
		},
		{
			name:     "create",
			mode:     AccessModeCreate,
			expected: ctf.O_CREATE,
		},
		{
			name:     "combined modes",
			mode:     AccessMode("readonly|create"),
			expected: ctf.O_RDONLY | ctf.O_CREATE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.ToAccessBitmask(); got != tt.expected {
				t.Errorf("AccessMode.ToAccessBitmask() = %v, want %v", got, tt.expected)
			}
		})
	}
}
