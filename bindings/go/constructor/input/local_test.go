package input

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockTargetRepository is a mock implementation of TargetRepository
type mockTargetRepository struct {
	mock.Mock
}

func (m *mockTargetRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	args := m.Called(ctx, component, version, res, content)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*descriptor.Resource), args.Error(1)
}

// mockReadOnlyBlob is a mock implementation of blob.ReadOnlyBlob
type mockReadOnlyBlob struct {
	content   []byte
	mediaType string
	size      int64
}

func (m *mockReadOnlyBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.content)), nil
}

func (m *mockReadOnlyBlob) Size() int64 {
	return m.size
}

func (m *mockReadOnlyBlob) MediaType() (string, bool) {
	return m.mediaType, m.mediaType != ""
}

func TestAddColocatedLocalBlob(t *testing.T) {
	tests := []struct {
		name           string
		component      string
		version        string
		resource       *spec.Resource
		blob           *mockReadOnlyBlob
		mockSetup      func(*mockTargetRepository)
		expectedError  string
		expectedResult *descriptor.Resource
	}{
		{
			name:      "successful addition with defaults",
			component: "test-component",
			version:   "v1.0.0",
			resource: &spec.Resource{
				ElementMeta: spec.ElementMeta{
					ObjectMeta: spec.ObjectMeta{
						Name: "test-resource",
					},
				},
				AccessOrInput: spec.AccessOrInput{
					Input: &runtime.Raw{
						Type: runtime.Type{
							Name:    "binary",
							Version: "v1",
						},
					},
				},
			},
			blob: &mockReadOnlyBlob{
				content:   []byte("test data"),
				mediaType: "application/test",
				size:      int64(len("test data")),
			},
			mockSetup: func(m *mockTargetRepository) {
				expectedResource := &descriptor.Resource{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-resource",
							Version: "v1.0.0",
						},
					},
					Relation: descriptor.LocalRelation,
					Size:     int64(len("test data")),
					Access: &v2.LocalBlob{
						Type: runtime.Type{
							Name:    v2.LocalBlobAccessType,
							Version: v2.LocalBlobAccessTypeVersion,
						},
						MediaType: "application/test",
					},
				}
				m.On("AddLocalResource", mock.Anything, "test-component", "v1.0.0", mock.MatchedBy(func(r *descriptor.Resource) bool {
					return r.Name == expectedResource.Name &&
						r.Version == expectedResource.Version &&
						r.Relation == expectedResource.Relation &&
						r.Size == expectedResource.Size &&
						r.Access.(*v2.LocalBlob).MediaType == expectedResource.Access.(*v2.LocalBlob).MediaType
				}), mock.Anything).Return(expectedResource, nil)
			},
			expectedResult: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "v1.0.0",
					},
				},
				Relation: descriptor.LocalRelation,
				Size:     int64(len("test data")),
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobAccessType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/test",
				},
			},
		},
		{
			name:      "successful addition with explicit fields",
			component: "test-component",
			version:   "v1.0.0",
			resource: &spec.Resource{
				ElementMeta: spec.ElementMeta{
					ObjectMeta: spec.ObjectMeta{
						Name:    "test-resource",
						Version: "custom-version",
					},
				},
				Relation: spec.ExternalRelation,
				AccessOrInput: spec.AccessOrInput{
					Input: &runtime.Raw{
						Type: runtime.Type{
							Name:    "binary",
							Version: "v1",
						},
					},
				},
			},
			blob: &mockReadOnlyBlob{
				content:   []byte("test data"),
				mediaType: "application/test",
				size:      int64(len("test data")),
			},
			mockSetup: func(m *mockTargetRepository) {
				expectedResource := &descriptor.Resource{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-resource",
							Version: "custom-version",
						},
					},
					Relation: descriptor.ExternalRelation,
					Size:     int64(len("test data")),
					Access: &v2.LocalBlob{
						Type: runtime.Type{
							Name:    v2.LocalBlobAccessType,
							Version: v2.LocalBlobAccessTypeVersion,
						},
						MediaType: "application/test",
					},
				}
				m.On("AddLocalResource", mock.Anything, "test-component", "v1.0.0", mock.MatchedBy(func(r *descriptor.Resource) bool {
					return r.Name == expectedResource.Name &&
						r.Version == expectedResource.Version &&
						r.Relation == expectedResource.Relation &&
						r.Size == expectedResource.Size &&
						r.Access.(*v2.LocalBlob).MediaType == expectedResource.Access.(*v2.LocalBlob).MediaType
				}), mock.Anything).Return(expectedResource, nil)
			},
			expectedResult: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "custom-version",
					},
				},
				Relation: descriptor.ExternalRelation,
				Size:     int64(len("test data")),
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobAccessType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType: "application/test",
				},
			},
		},
		{
			name:      "error from repository",
			component: "test-component",
			version:   "v1.0.0",
			resource: &spec.Resource{
				ElementMeta: spec.ElementMeta{
					ObjectMeta: spec.ObjectMeta{
						Name: "test-resource",
					},
				},
				AccessOrInput: spec.AccessOrInput{
					Input: &runtime.Raw{
						Type: runtime.Type{
							Name:    "binary",
							Version: "v1",
						},
					},
				},
			},
			blob: &mockReadOnlyBlob{
				content:   []byte("test data"),
				mediaType: "application/test",
				size:      int64(len("test data")),
			},
			mockSetup: func(m *mockTargetRepository) {
				m.On("AddLocalResource", mock.Anything, "test-component", "v1.0.0", mock.Anything, mock.Anything).
					Return(nil, errors.New("repository error"))
			},
			expectedError: "error adding local resource",
		},
		{
			name:      "blob without media type",
			component: "test-component",
			version:   "v1.0.0",
			resource: &spec.Resource{
				ElementMeta: spec.ElementMeta{
					ObjectMeta: spec.ObjectMeta{
						Name: "test-resource",
					},
				},
				AccessOrInput: spec.AccessOrInput{
					Input: &runtime.Raw{
						Type: runtime.Type{
							Name:    "binary",
							Version: "v1",
						},
					},
				},
			},
			blob: &mockReadOnlyBlob{
				content: []byte("test data"),
				size:    int64(len("test data")),
			},
			mockSetup: func(m *mockTargetRepository) {
				expectedResource := &descriptor.Resource{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-resource",
							Version: "v1.0.0",
						},
					},
					Relation: descriptor.LocalRelation,
					Size:     int64(len("test data")),
					Access: &v2.LocalBlob{
						Type: runtime.Type{
							Name:    v2.LocalBlobAccessType,
							Version: v2.LocalBlobAccessTypeVersion,
						},
					},
				}
				m.On("AddLocalResource", mock.Anything, "test-component", "v1.0.0", mock.MatchedBy(func(r *descriptor.Resource) bool {
					return r.Name == expectedResource.Name &&
						r.Version == expectedResource.Version &&
						r.Relation == expectedResource.Relation &&
						r.Size == expectedResource.Size
				}), mock.Anything).Return(expectedResource, nil)
			},
			expectedResult: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "v1.0.0",
					},
				},
				Relation: descriptor.LocalRelation,
				Size:     int64(len("test data")),
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobAccessType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := new(mockTargetRepository)
			tt.mockSetup(repo)

			result, err := AddColocatedLocalBlob(context.Background(), repo, tt.component, tt.version, tt.resource, tt.blob)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedResult, result)
			repo.AssertExpectations(t)
		})
	}
}
