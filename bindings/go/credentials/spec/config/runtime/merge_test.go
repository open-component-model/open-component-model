package runtime

import (
	"testing"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMerge(t *testing.T) {
	tests := []struct {
		name     string
		configs  []*Config
		expected *Config
	}{
		{
			name:     "empty configs",
			configs:  []*Config{},
			expected: &Config{},
		},
		{
			name: "single config",
			configs: []*Config{
				{
					Type: runtime.NewVersionedType("test-type", "v1"),
					Repositories: []RepositoryConfigEntry{
						{Repository: &mockTyped{name: "repo1"}},
					},
					Consumers: []Consumer{
						{
							Identities:  []runtime.Identity{runtime.Identity{"type": "id1"}},
							Credentials: []runtime.Typed{&mockTyped{name: "cred1"}},
						},
					},
				},
			},
			expected: &Config{
				Type: runtime.NewVersionedType("test-type", "v1"),
				Repositories: []RepositoryConfigEntry{
					{Repository: &mockTyped{name: "repo1"}},
				},
				Consumers: []Consumer{
					{
						Identities:  []runtime.Identity{runtime.Identity{"type": "id1"}},
						Credentials: []runtime.Typed{&mockTyped{name: "cred1"}},
					},
				},
			},
		},
		{
			name: "multiple configs",
			configs: []*Config{
				{
					Type: runtime.NewVersionedType("test-type", "v1"),
					Repositories: []RepositoryConfigEntry{
						{Repository: &mockTyped{name: "repo1"}},
					},
					Consumers: []Consumer{
						{
							Identities:  []runtime.Identity{runtime.Identity{"type": "id1"}},
							Credentials: []runtime.Typed{&mockTyped{name: "cred1"}},
						},
					},
				},
				{
					Type: runtime.NewVersionedType("test-type", "v1"),
					Repositories: []RepositoryConfigEntry{
						{Repository: &mockTyped{name: "repo2"}},
					},
					Consumers: []Consumer{
						{
							Identities:  []runtime.Identity{runtime.Identity{"type": "id2"}},
							Credentials: []runtime.Typed{&mockTyped{name: "cred2"}},
						},
					},
				},
			},
			expected: &Config{
				Type: runtime.NewVersionedType("test-type", "v1"),
				Repositories: []RepositoryConfigEntry{
					{Repository: &mockTyped{name: "repo1"}},
					{Repository: &mockTyped{name: "repo2"}},
				},
				Consumers: []Consumer{
					{
						Identities:  []runtime.Identity{runtime.Identity{"type": "id1"}},
						Credentials: []runtime.Typed{&mockTyped{name: "cred1"}},
					},
					{
						Identities:  []runtime.Identity{runtime.Identity{"type": "id2"}},
						Credentials: []runtime.Typed{&mockTyped{name: "cred2"}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Merge(tt.configs...)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("Merge() = %v, want nil", result)
				}
				return
			}

			if result.Type != tt.expected.Type {
				t.Errorf("Merge().Type = %v, want %v", result.Type, tt.expected.Type)
			}

			if len(result.Repositories) != len(tt.expected.Repositories) {
				t.Errorf("Merge().Repositories length = %v, want %v", len(result.Repositories), len(tt.expected.Repositories))
			}

			if len(result.Consumers) != len(tt.expected.Consumers) {
				t.Errorf("Merge().Consumers length = %v, want %v", len(result.Consumers), len(tt.expected.Consumers))
			}
		})
	}
}

// Mock implementations for testing
type mockTyped struct {
	name string
	typ  runtime.Type
}

func (m *mockTyped) GetType() runtime.Type {
	return m.typ
}

func (m *mockTyped) SetType(typ runtime.Type) {
	m.typ = typ
}

func (m *mockTyped) DeepCopyTyped() runtime.Typed {
	return &mockTyped{
		name: m.name,
		typ:  m.typ,
	}
}
