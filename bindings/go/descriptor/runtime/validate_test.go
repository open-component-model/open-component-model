package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		descriptor  *Descriptor
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid descriptor with no duplicates",
			descriptor: &Descriptor{
				Component: Component{
					Resources: []Resource{
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "resource1",
									Version: "1.0",
								},
							},
						},
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "resource2",
									Version: "1.0",
								},
							},
						},
					},
					Sources: []Source{
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "source1",
									Version: "1.0",
								},
							},
						},
					},
					References: []Reference{
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "ref1",
									Version: "1.0",
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "duplicate resource identities",
			descriptor: &Descriptor{
				Component: Component{
					Resources: []Resource{
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "resource1",
									Version: "1.0",
								},
							},
						},
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "resource1",
									Version: "1.0",
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "validation failed:\n- duplicate resource identities found: identity 'map[name:resource1 version:1.0]' appears 2 times at resource indices [0, 1]",
		},
		{
			name: "duplicate source identities with extra identity",
			descriptor: &Descriptor{
				Component: Component{
					Sources: []Source{
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "source1",
									Version: "1.0",
								},
								ExtraIdentity: runtime.Identity{
									"type": "git",
								},
							},
						},
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "source1",
									Version: "1.0",
								},
								ExtraIdentity: runtime.Identity{
									"type": "git",
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "validation failed:\n- duplicate source identities found: identity 'map[name:source1 type:git version:1.0]' appears 2 times at source indices [0, 1]",
		},
		{
			name: "multiple duplicate types",
			descriptor: &Descriptor{
				Component: Component{
					Resources: []Resource{
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "resource1",
									Version: "1.0",
								},
							},
						},
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "resource1",
									Version: "1.0",
								},
							},
						},
					},
					Sources: []Source{
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "source1",
									Version: "1.0",
								},
							},
						},
						{
							ElementMeta: ElementMeta{
								ObjectMeta: ObjectMeta{
									Name:    "source1",
									Version: "1.0",
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "validation failed:\n- duplicate resource identities found: identity 'map[name:resource1 version:1.0]' appears 2 times at resource indices [0, 1]\n- duplicate source identities found: identity 'map[name:source1 version:1.0]' appears 2 times at source indices [0, 1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.descriptor)
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, tt.errorMsg, err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
