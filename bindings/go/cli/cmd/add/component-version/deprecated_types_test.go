package componentversion

import (
	"bytes"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	gitaccess "ocm.software/open-component-model/bindings/go/git/spec/access"
	githubaccess "ocm.software/open-component-model/bindings/go/github/spec/access"
	fileinput "ocm.software/open-component-model/bindings/go/input/file"
	utf8input "ocm.software/open-component-model/bindings/go/input/utf8"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIsTypeDeprecated(t *testing.T) {
	tests := []struct {
		name     string
		scheme   *runtime.Scheme
		typ      string
		want     bool
		wantHint string
	}{
		{"whole type deprecated, canonical spelling", githubaccess.Scheme, "GitHub/v1", true, "use Git/v1 instead"},
		{"whole type deprecated, legacy spelling", githubaccess.Scheme, "github", true, "use Git/v1 instead"},
		{"Git canonical", gitaccess.Scheme, "Git/v1", false, ""},
		{"Git unversioned", gitaccess.Scheme, "Git", false, ""},
		{"Git legacy spelling", gitaccess.Scheme, "git", true, "use Git/v1 instead"},
		{"Git legacy version", gitaccess.Scheme, "Git/v1alpha1", true, "use Git/v1 instead"},
		{"unknown type", gitaccess.Scheme, "ociArtifact/v1", false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			typ, err := runtime.TypeFromString(tc.typ)
			r.NoError(err)
			deprecated, hint := isTypeDeprecated(tc.scheme, typ)
			r.Equal(tc.want, deprecated)
			r.Equal(tc.wantHint, hint)
		})
	}

	t.Run("a type without an embedded schema is not deprecated", func(t *testing.T) {
		r := require.New(t)
		scheme := runtime.NewScheme()
		scheme.MustRegister(&unregisteredAccess{}, "v1")
		deprecated, hint := isTypeDeprecated(scheme, runtime.NewVersionedType("unregisteredAccess", "v1"))
		r.False(deprecated)
		r.Empty(hint)
	})
}

func TestFindDeprecatedTypes(t *testing.T) {
	r := require.New(t)

	accessScheme := runtime.NewScheme()
	githubaccess.MustAddToScheme(accessScheme)
	gitaccess.MustAddToScheme(accessScheme)
	inputScheme := runtime.NewScheme()
	r.NoError(inputScheme.RegisterSchemes(fileinput.Scheme, utf8input.Scheme))

	typed := func(typ string) runtime.Typed {
		t, err := runtime.TypeFromString(typ)
		r.NoError(err)
		return &runtime.Raw{Type: t}
	}
	element := func(name string) constructorruntime.ElementMeta {
		return constructorruntime.ElementMeta{ObjectMeta: constructorruntime.ObjectMeta{Name: name, Version: "1.0.0"}}
	}
	spec := &constructorruntime.ComponentConstructor{Components: []constructorruntime.Component{{
		ComponentMeta: constructorruntime.ComponentMeta{ObjectMeta: constructorruntime.ObjectMeta{Name: "ocm.software/demo", Version: "1.0.0"}},
		Resources: []constructorruntime.Resource{
			{ElementMeta: element("old-resource"), AccessOrInput: constructorruntime.AccessOrInput{Access: typed("github")}},
			{ElementMeta: element("new-resource"), AccessOrInput: constructorruntime.AccessOrInput{Access: typed("Git/v1")}},
			{ElementMeta: element("old-input"), AccessOrInput: constructorruntime.AccessOrInput{Input: typed("file")}},
			{ElementMeta: element("new-input"), AccessOrInput: constructorruntime.AccessOrInput{Input: typed("utf8/v1")}},
			{ElementMeta: element("nothing")},
		},
		Sources: []constructorruntime.Source{
			{ElementMeta: element("old-source"), AccessOrInput: constructorruntime.AccessOrInput{Access: typed("GitHub/v1")}},
			{ElementMeta: element("old-source-again"), AccessOrInput: constructorruntime.AccessOrInput{Access: typed("github")}},
			{ElementMeta: element("new-source"), AccessOrInput: constructorruntime.AccessOrInput{Access: typed("Git")}},
			{ElementMeta: element("old-source-input"), AccessOrInput: constructorruntime.AccessOrInput{Input: typed("utf8")}},
		},
	}}}

	r.Equal([]string{
		"access type github is deprecated, use Git/v1 instead",
		"input type file is deprecated, use file/v1 instead",
		"access type GitHub/v1 is deprecated, use Git/v1 instead",
		"input type utf8 is deprecated, use utf8/v1 instead",
	}, findDeprecatedTypes(accessScheme, inputScheme, spec), "distinct access and input spellings in constructor order")
}

func TestRenderDeprecatedTypeNotice(t *testing.T) {
	t.Run("nothing deprecated prints nothing", func(t *testing.T) {
		r := require.New(t)
		var buf bytes.Buffer
		renderDeprecatedTypeNotice(&buf, nil)
		r.Empty(buf.String())
	})

	t.Run("a blank line and one yellow line per warning", func(t *testing.T) {
		r := require.New(t)
		var buf bytes.Buffer
		text.EnableColors()
		t.Cleanup(text.DisableColors)
		renderDeprecatedTypeNotice(&buf, []string{
			"access type GitHub/v1 is deprecated, use Git/v1 instead",
			"input type file is deprecated, use file/v1 instead",
		})
		r.Equal(
			"\n"+text.FgYellow.Sprint("WARNING: access type GitHub/v1 is deprecated, use Git/v1 instead")+"\n"+
				text.FgYellow.Sprint("WARNING: input type file is deprecated, use file/v1 instead")+"\n",
			buf.String())
	})

	t.Run("plain text when colors are off", func(t *testing.T) {
		r := require.New(t)
		var buf bytes.Buffer
		text.DisableColors()
		renderDeprecatedTypeNotice(&buf, []string{"input type utf8 is deprecated, use utf8/v1 instead"})
		r.Equal("\nWARNING: input type utf8 is deprecated, use utf8/v1 instead\n", buf.String())
	})
}
