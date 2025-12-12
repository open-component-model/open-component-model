package ctf

import (
	"encoding/json"
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
				FilePath: "./test/archive.tgz",
			},
			expected: "./test/archive.tgz",
		},
		{
			name: "absolute path",
			repo: Repository{
				FilePath: "/absolute/path/to/archive",
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

func TestRepository_UnmarshalJSON_WithNumericAccessMode(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected Repository
		wantErr  bool
	}{
		{
			name: "numeric accessMode 0",
			data: []byte(`{"type":"CommonTransportFormat","filePath":"./test.tgz","accessMode":0}`),
			expected: Repository{
				FilePath:   "./test.tgz",
				AccessMode: AccessModeReadOnly,
			},
			wantErr: false,
		},
		{
			name: "numeric accessMode 1",
			data: []byte(`{"type":"CommonTransportFormat","filePath":"./test.tgz","accessMode":1}`),
			expected: Repository{
				FilePath:   "./test.tgz",
				AccessMode: AccessModeReadWrite,
			},
			wantErr: false,
		},
		{
			name: "string accessMode",
			data: []byte(`{"type":"CommonTransportFormat","filePath":"./test.tgz","accessMode":"create"}`),
			expected: Repository{
				FilePath:   "./test.tgz",
				AccessMode: AccessModeCreate,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var repo Repository
			err := json.Unmarshal(tt.data, &repo)
			if (err != nil) != tt.wantErr {
				t.Errorf("Repository unmarshal error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if repo.FilePath != tt.expected.FilePath {
					t.Errorf("Repository.FilePath = %v, want %v", repo.FilePath, tt.expected.FilePath)
				}
				if repo.AccessMode != tt.expected.AccessMode {
					t.Errorf("Repository.AccessMode = %v, want %v", repo.AccessMode, tt.expected.AccessMode)
				}
			}
		})
	}
}
