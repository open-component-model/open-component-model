package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	githubv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesGitHubAccess(t *testing.T) {
	obj, err := scheme.NewObject(runtime.NewVersionedType(githubv1.LegacyType, githubv1.Version))
	require.NoError(t, err)
	_, ok := obj.(*githubv1.GitHub)
	require.True(t, ok, "transfer scheme must resolve a github access to *githubv1.GitHub")
}
