package v2_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func versionValidationDescriptor(componentVersion, resourceVersion string) *descriptorv2.Descriptor {
	return &descriptorv2.Descriptor{
		Meta: descriptorv2.Meta{Version: "v2"},
		Component: descriptorv2.Component{
			Provider: "example",
			ComponentMeta: descriptorv2.ComponentMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "github.com/example/component",
					Version: componentVersion,
				},
			},
			RepositoryContexts: []*runtime.Raw{},
			Resources: []descriptorv2.Resource{
				{
					ElementMeta: descriptorv2.ElementMeta{
						ObjectMeta: descriptorv2.ObjectMeta{Name: "res", Version: resourceVersion},
					},
					Type:     "blob",
					Relation: descriptorv2.LocalRelation,
				},
			},
			Sources:    []descriptorv2.Source{},
			References: []descriptorv2.Reference{},
		},
	}
}

func TestValidateVersions_DefaultSemver(t *testing.T) {
	r := require.New(t)

	// Semver versions pass with the default (nil → loose semver) registry.
	r.NoError(descriptorv2.ValidateVersions(versionValidationDescriptor("1.0.0", "1.0.0"), nil))
	// A non-semver component version fails against the default registry.
	err := descriptorv2.ValidateVersions(versionValidationDescriptor("garbage version", "1.0.0"), nil)
	r.Error(err)
	r.Contains(err.Error(), `component "github.com/example/component" has an invalid version "garbage version"`)

	// A non-semver resource version fails too.
	err = descriptorv2.ValidateVersions(versionValidationDescriptor("1.0.0", "not-a-version"), nil)
	r.Error(err)
	r.Contains(err.Error(), `resource "res" has an invalid version "not-a-version"`)
}

func TestValidateVersions_CalverRegistry(t *testing.T) {
	r := require.New(t)
	reg := versioning.NewRegistry(
		versioning.NewRegexScheme("calver",
			regexp.MustCompile(`^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$`),
			[]string{"year", "month", "day"}),
		versioning.Default().Schemes()[0],
	)

	// calver component + semver resource both accepted by the configured registry.
	r.NoError(descriptorv2.ValidateVersions(versionValidationDescriptor("2024.03.15", "1.0.0"), reg))

	// a version matching neither scheme still fails.
	err := descriptorv2.ValidateVersions(versionValidationDescriptor("garbage version", "1.0.0"), reg)
	r.Error(err)
}
