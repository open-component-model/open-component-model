package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
)

func Test_Integration_AddComponentVersion_OCIRepository(t *testing.T) {
	suite := SetupTestSuite(t)

	t.Run("add component-version with plain OCI registry reference", func(t *testing.T) {
		r := require.New(t)

		componentName := "ocm.software/test-component"
		componentVersion := "v1.0.0"
		
		// Create constructor file using suite helper
		constructorPath := suite.CreateComponentConstructor(t, componentName, componentVersion)

		// Test the add component-version command with plain OCI registry reference
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", suite.GetRepositoryURL(),
			"--constructor", constructorPath,
			"--config", suite.ConfigPath,
		})

		r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with OCI registry")

		// Verify the component version was added by attempting to retrieve it
		desc, err := suite.Repository.GetComponentVersion(t.Context(), componentName, componentVersion)
		r.NoError(err, "should be able to retrieve the added component version")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Equal("ocm.software", desc.Component.Provider.Name)
		r.Len(desc.Component.Resources, 1)
		r.Equal("test-resource", desc.Component.Resources[0].Name)
		r.Equal(componentVersion, desc.Component.Resources[0].Version)
	})

	t.Run("add component-version with explicit OCI type prefix", func(t *testing.T) {
		r := require.New(t)

		componentName := "ocm.software/explicit-oci-component"
		componentVersion := "v2.0.0"
		
		// Create constructor file using suite helper
		constructorPath := suite.CreateComponentConstructor(t, componentName, componentVersion)

		// Test with explicit oci:: prefix
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", suite.GetRepositoryURLWithPrefix("oci"),
			"--constructor", constructorPath,
			"--config", suite.ConfigPath,
		})

		r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with explicit OCI type")

		// Verify the component version was added
		desc, err := suite.Repository.GetComponentVersion(t.Context(), componentName, componentVersion)
		r.NoError(err, "should be able to retrieve the component version added with explicit OCI type")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
	})

	t.Run("add component-version with HTTP URL format", func(t *testing.T) {
		r := require.New(t)

		componentName := "ocm.software/http-component"
		componentVersion := "v3.0.0"
		
		// Create constructor file using suite helper
		constructorPath := suite.CreateComponentConstructor(t, componentName, componentVersion)

		// Test with HTTP URL format
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", suite.GetRepositoryURL(),
			"--constructor", constructorPath,
			"--config", suite.ConfigPath,
		})

		r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with HTTP URL format")

		// Verify the component version was added
		desc, err := suite.Repository.GetComponentVersion(t.Context(), componentName, componentVersion)
		r.NoError(err, "should be able to retrieve the component version added with HTTP URL")
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
	})
}

func Test_Integration_AddComponentVersion_CTFRepository(t *testing.T) {
	t.Run("add component-version with CTF archive path", func(t *testing.T) {
		r := require.New(t)

		componentName := "ocm.software/ctf-component"
		componentVersion := "v1.0.0"
		
		// Create temporary test suite for this test (no shared registry needed for CTF)
		suite := &TestSuite{}
		constructorPath := suite.CreateComponentConstructor(t, componentName, componentVersion)

		// Test with CTF archive path
		ctfArchivePath := filepath.Join(t.TempDir(), "test-archive")
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", ctfArchivePath,
			"--constructor", constructorPath,
		})

		r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with CTF archive")

		// Verify the archive was created
		_, err := os.Stat(ctfArchivePath)
		r.NoError(err, "CTF archive should be created")
	})

	t.Run("add component-version with explicit CTF type prefix", func(t *testing.T) {
		r := require.New(t)

		componentName := "ocm.software/explicit-ctf-component"
		componentVersion := "v2.0.0"
		
		// Create temporary test suite for this test (no shared registry needed for CTF)
		suite := &TestSuite{}
		constructorPath := suite.CreateComponentConstructor(t, componentName, componentVersion)

		// Test with explicit ctf:: prefix
		ctfArchivePath := filepath.Join(t.TempDir(), "explicit-archive")
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add",
			"component-version",
			"--repository", fmt.Sprintf("ctf::%s", ctfArchivePath),
			"--constructor", constructorPath,
		})

		r.NoError(addCMD.ExecuteContext(t.Context()), "add component-version should succeed with explicit CTF type")

		// Verify the archive was created
		_, err := os.Stat(ctfArchivePath)
		r.NoError(err, "CTF archive should be created with explicit type")
	})
}