package v1alpha1_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newPyPI() *v1alpha1.PyPI {
	return &v1alpha1.PyPI{
		IndexURL: "https://pypi.org/simple",
		Project:  "requests",
		Version:  "2.32.3",
	}
}

func TestValidate(t *testing.T) {
	t.Run("valid without distributions", func(t *testing.T) {
		require.NoError(t, newPyPI().Validate())
	})
	t.Run("valid with distributions", func(t *testing.T) {
		p := newPyPI()
		p.Distributions = []v1alpha1.Distribution{
			{Kind: v1alpha1.KindWheel},
			{Kind: v1alpha1.KindSdist},
			{Filename: "requests-2.32.3-py3-none-any.whl"},
		}
		require.NoError(t, p.Validate())
	})
	t.Run("missing project", func(t *testing.T) {
		p := newPyPI()
		p.Project = ""
		require.ErrorContains(t, p.Validate(), "project")
	})
	t.Run("missing version", func(t *testing.T) {
		p := newPyPI()
		p.Version = ""
		require.ErrorContains(t, p.Validate(), "version")
	})
	t.Run("missing indexUrl", func(t *testing.T) {
		p := newPyPI()
		p.IndexURL = ""
		require.ErrorContains(t, p.Validate(), "indexUrl")
	})
	t.Run("relative indexUrl", func(t *testing.T) {
		p := newPyPI()
		p.IndexURL = "/simple"
		require.ErrorContains(t, p.Validate(), "absolute URL")
	})
	t.Run("unparseable indexUrl", func(t *testing.T) {
		p := newPyPI()
		p.IndexURL = "://bad"
		require.ErrorContains(t, p.Validate(), "indexUrl")
	})
	t.Run("bad kind", func(t *testing.T) {
		p := newPyPI()
		p.Distributions = []v1alpha1.Distribution{{Kind: "egg"}}
		require.ErrorContains(t, p.Validate(), `kind "egg"`)
	})
	t.Run("duplicate distribution", func(t *testing.T) {
		p := newPyPI()
		p.Distributions = []v1alpha1.Distribution{{Kind: v1alpha1.KindWheel}, {Kind: v1alpha1.KindWheel}}
		require.ErrorContains(t, p.Validate(), "distributions[1]: duplicate entry")
	})
	t.Run("filename precedence: bad kind ignored when filename set", func(t *testing.T) {
		p := newPyPI()
		p.Distributions = []v1alpha1.Distribution{{Kind: "egg", Filename: "requests-2.32.3.tar.gz"}}
		require.NoError(t, p.Validate())
	})
	t.Run("path traversal in project or version", func(t *testing.T) {
		for name, mutate := range map[string]func(*v1alpha1.PyPI){
			"project": func(p *v1alpha1.PyPI) { p.Project = "../../secret" },
			"version": func(p *v1alpha1.PyPI) { p.Version = `1.0\..` },
		} {
			p := newPyPI()
			mutate(p)
			err := p.Validate()
			require.ErrorContains(t, err, name)
			require.ErrorContains(t, err, "path separators")
		}
	})
	t.Run("path traversal in filename", func(t *testing.T) {
		p := newPyPI()
		p.Distributions = []v1alpha1.Distribution{{Filename: "../evil.whl"}}
		require.ErrorContains(t, p.Validate(), "path separators")
	})
	t.Run("errors are joined", func(t *testing.T) {
		err := (&v1alpha1.PyPI{}).Validate()
		require.Error(t, err)
		for _, want := range []string{"project", "version", "indexUrl"} {
			assert.Contains(t, err.Error(), want)
		}
	})
}

func TestPyPI_IsPinnedVersion(t *testing.T) {
	for _, version := range []string{"2.32.3", "1.0.0rc1", "0.0.0.dev1"} {
		p := newPyPI()
		p.Version = version
		assert.True(t, p.IsPinnedVersion(), version)
	}
}

func TestNormalizeProjectName(t *testing.T) {
	for input, want := range map[string]string{
		"requests":      "requests",
		"Foo.Bar_Baz":   "foo-bar-baz",
		"Django":        "django",
		"zope.event":    "zope-event",
		"a---b___c...d": "a-b-c-d",
	} {
		assert.Equal(t, want, v1alpha1.NormalizeProjectName(input), input)
	}
}

func TestPyPI_JSONRoundTrip(t *testing.T) {
	in := v1alpha1.PyPI{
		Type:     runtime.NewVersionedType(v1alpha1.Type, v1alpha1.Version),
		IndexURL: "https://pypi.org/simple",
		Project:  "requests",
		Version:  "2.32.3",
		Distributions: []v1alpha1.Distribution{
			{Kind: v1alpha1.KindWheel},
			{Filename: "requests-2.32.3.tar.gz"},
		},
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type": "pypi/v1alpha1",
		"indexUrl": "https://pypi.org/simple",
		"project": "requests",
		"version": "2.32.3",
		"distributions": [
			{"kind": "wheel"},
			{"filename": "requests-2.32.3.tar.gz"}
		]
	}`, string(data))

	var out v1alpha1.PyPI
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

func TestPyPI_DeepCopyIsolatesDistributions(t *testing.T) {
	in := newPyPI()
	in.Distributions = []v1alpha1.Distribution{{Kind: v1alpha1.KindWheel}}
	out := in.DeepCopy()
	out.Distributions[0].Kind = v1alpha1.KindSdist
	assert.Equal(t, v1alpha1.KindWheel, in.Distributions[0].Kind)
}
