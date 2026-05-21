package owner

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
)

func Test_ocmRepositoryBase(t *testing.T) {
	tests := []struct {
		name        string
		imageRef    string
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:     "registry with single-segment path and digest",
			imageRef: "ghcr.io/component-descriptors/ocm.software/app@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			want:     "ghcr.io",
		},
		{
			name:     "registry with nested path and digest",
			imageRef: "ghcr.io/acme/component-descriptors/ocm.software/app@sha256:2222222222222222222222222222222222222222222222222222222222222222",
			want:     "ghcr.io/acme",
		},
		{
			name:     "registry with deep sub-path and tag",
			imageRef: "registry.example.com/team/proj/component-descriptors/ocm.software/app:1.0.0",
			want:     "registry.example.com/team/proj",
		},
		{
			name:     "http scheme is preserved and prefixed with oci::",
			imageRef: "http://localhost:5000/acme/component-descriptors/ocm.software/app@sha256:3333333333333333333333333333333333333333333333333333333333333333",
			want:     "oci::http://localhost:5000/acme",
		},
		{
			name:     "https scheme is preserved and prefixed with oci::",
			imageRef: "https://registry.example.com/component-descriptors/ocm.software/app:1.0.0",
			want:     "oci::https://registry.example.com",
		},
		{
			// Guards against the LastIndex fix in ocmRepositoryBase: a path
			// segment containing `component-descriptors` as a substring (here,
			// `component-descriptors-archive`) must not shadow the real OCM
			// layout marker that follows.
			name:     "registry path with component-descriptors as substring does not shadow the marker",
			imageRef: "ghcr.io/mirrors/component-descriptors-archive/component-descriptors/ocm.software/app@sha256:5555555555555555555555555555555555555555555555555555555555555555",
			want:     "ghcr.io/mirrors/component-descriptors-archive",
		},
		{
			name:        "missing component-descriptors marker is rejected",
			imageRef:    "ghcr.io/acme/foo@sha256:4444444444444444444444444444444444444444444444444444444444444444",
			wantErr:     true,
			errContains: "not in the OCM",
		},
		{
			name:        "invalid image reference fails parsing",
			imageRef:    "://bad",
			wantErr:     true,
			errContains: "parsing image reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ocmRepositoryBase(tt.imageRef)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_New_CommandStructure(t *testing.T) {
	cmd := New()

	require.NotNil(t, cmd)
	assert.Equal(t, "owner {ref}", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.DisableAutoGenTag)
	assert.NotNil(t, cmd.RunE, "RunE must be wired so the command actually executes")
}

func Test_New_ArgsValidation(t *testing.T) {
	cmd := New()
	require.NotNil(t, cmd.Args, "command must reject malformed arg counts")

	t.Run("zero args is rejected", func(t *testing.T) {
		assert.Error(t, cmd.Args(cmd, []string{}))
	})
	t.Run("exactly one arg is accepted", func(t *testing.T) {
		assert.NoError(t, cmd.Args(cmd, []string{"ghcr.io/foo@sha256:abc"}))
	})
	t.Run("two args are rejected", func(t *testing.T) {
		assert.Error(t, cmd.Args(cmd, []string{"a", "b"}))
	})
}

func Test_New_OutputFlag(t *testing.T) {
	cmd := New()

	flag := cmd.Flags().Lookup(FlagOutput)
	require.NotNil(t, flag, "--output flag must be registered")
	assert.Equal(t, "o", flag.Shorthand)

	// Default value is the first registered option ("table").
	got, err := enum.Get(cmd.Flags(), FlagOutput)
	require.NoError(t, err)
	assert.Equal(t, "table", got)

	t.Run("accepts table, yaml, and json", func(t *testing.T) {
		for _, v := range []string{"table", "yaml", "json"} {
			require.NoError(t, cmd.Flags().Set(FlagOutput, v), "expected %q to be a valid value", v)
		}
	})

	t.Run("rejects unknown values", func(t *testing.T) {
		err := cmd.Flags().Set(FlagOutput, "xml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected one of")
	})
}

// Test_GetOwner_MissingPluginManager drives GetOwner with a bare context
// (no ocmctx.Context attached) and asserts the early error message. The
// plugin-manager check is the first nil guard in GetOwner — same ordering
// as `get cv`.
func Test_GetOwner_MissingPluginManager(t *testing.T) {
	cmd := New()
	cmd.SetContext(context.Background())

	err := GetOwner(cmd, []string{"ghcr.io/component-descriptors/ocm.software/app:1.0.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin manager")
}

// Test_writeOwnersJSON verifies the `-o json` output is an indented JSON
// array of the backend-neutral owner-lookup payloads — not the resolved
// component descriptors. This is the contract that lets callers use
// `-o json` to inspect ownership referrers without paying for a cv lookup.
func Test_writeOwnersJSON(t *testing.T) {
	owners := []repository.ResourceOwner{
		{
			ComponentName:    "ocm.software/a",
			ComponentVersion: "1.0.0",
			Artifact: repository.ResourceOwnerArtifact{
				Identity: map[string]string{"name": "res-a"},
				Kind:     "resource",
			},
		},
		{ComponentName: "ocm.software/b", ComponentVersion: "2.0.0"},
	}

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)

	require.NoError(t, writeOwnersJSON(cmd, owners))

	// Decode into a generic shape and assert on the wire-level key names so a
	// rename like `componentName` -> `component_name` would fail here even
	// though a round-trip through []repository.ResourceOwner would still pass.
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded, 2)
	assert.Equal(t, "ocm.software/a", decoded[0]["componentName"])
	assert.Equal(t, "1.0.0", decoded[0]["componentVersion"])
	require.Contains(t, decoded[0], "artifact", "artifact key must be present")
	artifact, ok := decoded[0]["artifact"].(map[string]any)
	require.True(t, ok, "artifact must be a JSON object")
	assert.Equal(t, "resource", artifact["kind"])
	assert.Contains(t, artifact, "identity")

	// Output must be pretty-printed (indented) for human consumption.
	assert.Contains(t, buf.String(), "\n  ")
}

// Test_GetOwner_MissingCredentialGraph verifies that GetOwner short-circuits
// when the OCM context has a plugin manager but no credential graph. Mirrors
// the second nil-guard in `get cv`.
func Test_GetOwner_MissingCredentialGraph(t *testing.T) {
	cmd := New()
	pm := manager.NewPluginManager(context.Background())
	ctx, err := ocmctx.WithPluginManager(context.Background(), pm)
	require.NoError(t, err)
	cmd.SetContext(ctx)

	err = GetOwner(cmd, []string{"ghcr.io/component-descriptors/ocm.software/app:1.0.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential graph")
}

// Test_writeNoOwners locks the fixed-format message GetOwner emits when the
// ownership lookup returns an empty set. Extracting the message into its own
// helper made this branch testable without standing up the plugin registry.
func Test_writeNoOwners(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)

	require.NoError(t, writeNoOwners(cmd, "oci::ghcr.io/acme/component-descriptors/foo:1.0.0"))
	assert.Equal(t, "no owning component version found for oci::ghcr.io/acme/component-descriptors/foo:1.0.0\n", buf.String())
}

// Test_ownerRefs covers the dedup behavior — the whole point of the function.
// The integration test only exercises the single-owner happy path.
func Test_ownerRefs(t *testing.T) {
	tests := []struct {
		name     string
		repoBase string
		owners   []repository.ResourceOwner
		want     []string
	}{
		{
			name:     "empty owners returns empty slice",
			repoBase: "ghcr.io/acme",
			owners:   []repository.ResourceOwner{},
			want:     []string{},
		},
		{
			name:     "distinct owners preserved in input order",
			repoBase: "ghcr.io/acme",
			owners: []repository.ResourceOwner{
				{ComponentName: "ocm.software/a", ComponentVersion: "1.0.0"},
				{ComponentName: "ocm.software/b", ComponentVersion: "2.0.0"},
			},
			want: []string{
				"ghcr.io/acme//ocm.software/a:1.0.0",
				"ghcr.io/acme//ocm.software/b:2.0.0",
			},
		},
		{
			name:     "duplicate (name, version) collapses regardless of artifact identity",
			repoBase: "ghcr.io/acme",
			owners: []repository.ResourceOwner{
				{ComponentName: "ocm.software/a", ComponentVersion: "1.0.0"},
				{ComponentName: "ocm.software/b", ComponentVersion: "2.0.0"},
				{
					ComponentName:    "ocm.software/a",
					ComponentVersion: "1.0.0",
					Artifact:         repository.ResourceOwnerArtifact{Identity: map[string]string{"name": "other-artifact"}},
				},
			},
			want: []string{
				"ghcr.io/acme//ocm.software/a:1.0.0",
				"ghcr.io/acme//ocm.software/b:2.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ownerRefs(tt.repoBase, tt.owners)
			assert.Equal(t, tt.want, got)
		})
	}
}
