package compref

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Test_WindowsPaths tests Windows-specific path handling by simulating Windows OS behavior.
// This allows testing the Windows code paths without requiring a Windows runner.
// Regression tests for https://github.com/open-component-model/open-component-model/issues/1776
func Test_WindowsPaths(t *testing.T) {
	t.Run("Parse", func(t *testing.T) {
		cases := []struct {
			input    string
			expected *Ref
		}{
			{
				input: `C:\TEMP\ctf\component-descriptors\ocm.software/cli:0.1.0`,
				expected: &Ref{
					Type: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(),
					Repository: &ctfv1.Repository{
						FilePath: "C:/TEMP/ctf",
					},
					Prefix:    "component-descriptors",
					Component: "ocm.software/cli",
					Version:   "0.1.0",
				},
			},
			{
				input: `D:/TEMP/ctf/component-descriptors/ocm.software/cli:0.1.0`,
				expected: &Ref{
					Type: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(),
					Repository: &ctfv1.Repository{
						FilePath: "D:/TEMP/ctf",
					},
					Prefix:    "component-descriptors",
					Component: "ocm.software/cli",
					Version:   "0.1.0",
				},
			},
			{
				input: `C:\TEMP\ctf//ocm.software/cli:0.1.0`,
				expected: &Ref{
					Type: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(),
					Repository: &ctfv1.Repository{
						FilePath: "C:/TEMP/ctf",
					},
					Component: "ocm.software/cli",
					Version:   "0.1.0",
				},
			},
		}

		for i, tc := range cases {
			t.Run(fmt.Sprintf("case-%02d", i+1), func(t *testing.T) {
				t.Logf("%q", tc.input)
				r := require.New(t)
				parsed, err := Parse(tc.input)
				if tc.expected.Type != "" {
					if typ, err := runtime.TypeFromString(parsed.Type); err == nil {
						tc.expected.Repository.SetType(typ)
					}
				}
				r.NoError(err)
				r.Equalf(tc.expected, parsed, "input %q was incorrectly parsed", tc.input)
				r.Contains(parsed.String(), tc.expected.Component, "input %q did not serialize properly", tc.input)
			})
		}
	})

	t.Run("ParseRepository", func(t *testing.T) {
		tests := []struct {
			name           string
			repoRef        string
			expectedType   runtime.Type
			validateResult func(t *testing.T, result runtime.Typed, repoSpec string)
		}{
			{
				name:         "Windows absolute path with backslash",
				repoRef:      `C:\TEMP\ctf`,
				expectedType: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version),
				validateResult: func(t *testing.T, result runtime.Typed, repoSpec string) {
					repo, ok := result.(*ctfv1.Repository)
					require.True(t, ok, "expected *ctfv1.Repository")
					require.Equal(t, repoSpec, repo.FilePath)
				},
			},
			{
				name:         "Windows absolute path with forward slash",
				repoRef:      `D:/TEMP/ctf`,
				expectedType: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version),
				validateResult: func(t *testing.T, result runtime.Typed, repoSpec string) {
					repo, ok := result.(*ctfv1.Repository)
					require.True(t, ok, "expected *ctfv1.Repository")
					require.Equal(t, repoSpec, repo.FilePath)
				},
			},
			{
				name:         "Windows path with nested directories",
				repoRef:      `C:\Users\test\repos\my-archive`,
				expectedType: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version),
				validateResult: func(t *testing.T, result runtime.Typed, repoSpec string) {
					repo, ok := result.(*ctfv1.Repository)
					require.True(t, ok, "expected *ctfv1.Repository")
					require.Equal(t, repoSpec, repo.FilePath)
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := require.New(t)
				result, err := ParseRepository(tt.repoRef)
				r.NoError(err, "unexpected error: %v", err)
				r.NotNil(result, "expected non-nil result")
				r.Equal(tt.expectedType, result.GetType(), "unexpected repository type")
				tt.validateResult(t, result, tt.repoRef)
			})
		}
	})
}
