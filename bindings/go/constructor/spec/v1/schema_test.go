package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComponentConstructorSchema(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid component constructor with single component",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0",
				"provider": {
					"name": "acme"
				},
				"resources": [
					{
						"name": "resource1",
						"type": "plain",
						"relation": "local",
						"input": {
							"type": "file",
							"path": "testdata/text.txt"
						}
					}
				]
			}`,
			wantErr: false,
		},
		{
			name: "valid component constructor with component list",
			json: `{
				"components": [
					{
						"name": "github.com/acme.org/component",
						"version": "1.0.0",
						"provider": {
							"name": "acme"
						}
					}
				]
			}`,
			wantErr: false,
		},
		{
			name: "invalid component name format",
			json: `{
				"name": "InvalidName",
				"version": "1.0.0",
				"provider": { "name": "acme" }
			}`,
			wantErr: true,
		},
		{
			name: "invalid version format",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "invalid-version",
				"provider": { "name": "acme" }
			}`,
			wantErr: true,
		},
		{
			name: "valid version format with suffix",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "0.0.0-main",
				"provider": { "name": "acme" }
			}`,
			wantErr: false,
		},
		{
			name: "invalid version format with suffix",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "0.0.0_main",
				"provider": { "name": "acme" }
			}`,
			wantErr: true,
		},
		{
			name: "missing provider",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0"
			}`,
			wantErr: true,
		},
		{
			name: "resource with input and external relation mismatch",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0",
				"provider": { "name": "acme" },
				"resources": [
					{
						"name": "res1",
						"type": "plain",
						"relation": "external",
						"input": {
							"type": "file"
						}
					}
				]
			}`,
			wantErr: true,
		},
		{
			name: "resource with access but no relation",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0",
				"provider": { "name": "acme" },
				"resources": [
					{
						"name": "res1",
						"type": "plain",
						"access": {
							"type": "ociArtifact"
						}
					}
				]
			}`,
			wantErr: true,
		},
		{
			name: "resource with input only (valid implied local)",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0",
				"provider": { "name": "acme" },
				"resources": [
					{
						"name": "res1",
						"type": "plain",
						"input": {
							"type": "file"
						}
					}
				]
			}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRawJSON([]byte(tt.json))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// verify it unmarshals
				var cc ComponentConstructor
				err = json.Unmarshal([]byte(tt.json), &cc)
				require.NoError(t, err)
			}
		})
	}
}

func TestUnmarshalJSONUnsafe(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "valid component constructor",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0",
				"provider": {
					"name": "acme"
				}
			}`,
			wantErr: false,
		},
		{
			name: "partial component constructor (missing provider)",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0"
			}`,
			wantErr: false,
		},
		{
			name: "invalid component name format",
			json: `{
				"name": "InvalidName",
				"version": "1.0.0",
				"provider": { "name": "acme" }
			}`,
			wantErr: false,
		},
		{
			name: "malformed json",
			json: `{
				"name": "github.com/acme.org/component",
				"version": "1.0.0",
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cc ComponentConstructor
			err := cc.UnmarshalJSONUnsafe([]byte(tt.json))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, cc.Components)
				assert.Equal(t, "1.0.0", cc.Components[0].Version)
			}
		})
	}
}
