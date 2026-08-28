package constructor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// validatedAccess is a registered access type that checks its own required field.
type validatedAccess struct {
	Type    runtime.Type `json:"type"`
	URL     string       `json:"url,omitempty"`
	Retries int          `json:"retries,omitempty"`
}

func (a *validatedAccess) GetType() runtime.Type  { return a.Type }
func (a *validatedAccess) SetType(t runtime.Type) { a.Type = t }
func (a *validatedAccess) DeepCopyTyped() runtime.Typed {
	copied := *a
	return &copied
}

func (a *validatedAccess) Validate() error {
	if a.URL == "" {
		return errors.New("url is required")
	}
	return nil
}

// validatedInput is a registered input type that checks its own required field.
type validatedInput struct {
	Type runtime.Type `json:"type"`
	Path string       `json:"path,omitempty"`
}

func (i *validatedInput) GetType() runtime.Type  { return i.Type }
func (i *validatedInput) SetType(t runtime.Type) { i.Type = t }
func (i *validatedInput) DeepCopyTyped() runtime.Typed {
	copied := *i
	return &copied
}

func (i *validatedInput) Validate() error {
	if i.Path == "" {
		return errors.New("path is required")
	}
	return nil
}

// unvalidatedAccess is a registered access type that does not implement runtime.Validatable.
type unvalidatedAccess struct {
	Type runtime.Type `json:"type"`
	URL  string       `json:"url,omitempty"`
}

func (a *unvalidatedAccess) GetType() runtime.Type  { return a.Type }
func (a *unvalidatedAccess) SetType(t runtime.Type) { a.Type = t }
func (a *unvalidatedAccess) DeepCopyTyped() runtime.Typed {
	copied := *a
	return &copied
}

var (
	_ runtime.Validatable = (*validatedAccess)(nil)
	_ runtime.Validatable = (*validatedInput)(nil)
)

func validationOptionsForTest() Options {
	accessScheme := runtime.NewScheme(runtime.WithAllowUnknown())
	accessScheme.MustRegister(&validatedAccess{}, "v1")
	accessScheme.MustRegister(&unvalidatedAccess{}, "v1")

	inputScheme := runtime.NewScheme(runtime.WithAllowUnknown())
	inputScheme.MustRegister(&validatedInput{}, "v1")

	return Options{
		AccessSpecificationScheme: accessScheme,
		InputSpecificationScheme:  inputScheme,
	}
}

func constructorFromYAML(t *testing.T, data string) *constructorruntime.ComponentConstructor {
	t.Helper()
	spec := constructorv1.ComponentConstructor{}
	require.NoError(t, yaml.Unmarshal([]byte(data), &spec))
	return constructorruntime.ConvertToRuntimeConstructor(&spec)
}

func TestValidateSpecifications(t *testing.T) {
	t.Parallel()

	t.Run("valid access and input specifications pass", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: by-reference
    type: blob
    version: 1.0.0
    access:
      type: validatedAccess/v1
      url: https://ocm.software
  sources:
  - name: by-input
    type: blob
    version: 1.0.0
    input:
      type: validatedInput/v1
      path: ./some/path
`)
		require.NoError(t, validateSpecifications(componentConstructor, validationOptionsForTest()))
	})

	t.Run("a known access type that fails its own Validate is rejected", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: by-reference
    type: blob
    version: 1.0.0
    access:
      type: validatedAccess/v1
`)
		err := validateSpecifications(componentConstructor, validationOptionsForTest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), `resource "name=by-reference,version=1.0.0"`)
		assert.Contains(t, err.Error(), `access specification: type "validatedAccess/v1" is invalid`)
		assert.Contains(t, err.Error(), "url is required")
	})

	t.Run("a known input type that fails its own Validate is rejected", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: by-input
    type: blob
    version: 1.0.0
    input:
      type: validatedInput/v1
`)
		err := validateSpecifications(componentConstructor, validationOptionsForTest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), `input specification: type "validatedInput/v1" is invalid`)
		assert.Contains(t, err.Error(), "path is required")
	})

	t.Run("a specification that does not decode into its type is rejected", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: by-reference
    type: blob
    version: 1.0.0
    access:
      type: validatedAccess/v1
      url: https://ocm.software
      retries: many
`)
		err := validateSpecifications(componentConstructor, validationOptionsForTest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), `access specification: type "validatedAccess/v1" cannot be decoded`)
		assert.Contains(t, err.Error(), "retries")
	})

	t.Run("all violations are reported at once", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: first
    type: blob
    version: 1.0.0
    access:
      type: validatedAccess/v1
  - name: second
    type: blob
    version: 1.0.0
    access:
      type: validatedAccess/v1
`)
		err := validateSpecifications(componentConstructor, validationOptionsForTest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), `resource "name=first,version=1.0.0"`)
		assert.Contains(t, err.Error(), `resource "name=second,version=1.0.0"`)
	})

	t.Run("unknown types are not validated", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: custom
    type: blob
    version: 1.0.0
    access:
      type: my.custom.access/v1
      whatever: true
`)
		require.NoError(t, validateSpecifications(componentConstructor, validationOptionsForTest()))
	})

	t.Run("a known type without Validate is not validated beyond decoding", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: by-reference
    type: blob
    version: 1.0.0
    access:
      type: unvalidatedAccess/v1
`)
		require.NoError(t, validateSpecifications(componentConstructor, validationOptionsForTest()))
	})

	t.Run("without schemes nothing is validated", func(t *testing.T) {
		t.Parallel()
		componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: by-reference
    type: blob
    version: 1.0.0
    access:
      type: validatedAccess/v1
`)
		require.NoError(t, validateSpecifications(componentConstructor, Options{}))
	})
}

func TestConstructRejectsInvalidSpecifications(t *testing.T) {
	t.Parallel()

	componentConstructor := constructorFromYAML(t, `
components:
- name: ocm.software/test
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: by-reference
    type: blob
    version: 1.0.0
    access:
      type: validatedAccess/v1
`)

	constr := NewDefaultConstructor(componentConstructor, validationOptionsForTest())

	err := constr.Construct(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate component constructor specifications")
	assert.Contains(t, err.Error(), "url is required")
}
