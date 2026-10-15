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

// schemaless is a typed object that embeds no JSON schema.
type schemaless struct {
	Type runtime.Type `json:"type"`
}

func (s *schemaless) GetType() runtime.Type        { return s.Type }
func (s *schemaless) SetType(t runtime.Type)       { s.Type = t }
func (s *schemaless) DeepCopyTyped() runtime.Typed { c := *s; return &c }

// rootDeprecated embeds a schema that flags the whole type, as a "Deprecated:"
// doc paragraph does, without listing any spelling.
type rootDeprecated struct {
	Type runtime.Type `json:"type"`
}

func (s *rootDeprecated) GetType() runtime.Type        { return s.Type }
func (s *rootDeprecated) SetType(t runtime.Type)       { s.Type = t }
func (s *rootDeprecated) DeepCopyTyped() runtime.Typed { c := *s; return &c }
func (s *rootDeprecated) JSONSchema() []byte           { return []byte(`{"deprecated": true}`) }

func TestIsTypeDeprecated(t *testing.T) {
	tests := []struct {
		name     string
		scheme   *runtime.Scheme
		typ      string
		want     bool
		wantHint string
	}{
		{"GitHub canonical", githubaccess.Scheme, "GitHub/v1", true, "use Git/v1 instead"},
		{"GitHub unversioned", githubaccess.Scheme, "GitHub", true, "use Git/v1 instead"},
		{"GitHub legacy", githubaccess.Scheme, "github/v1", true, "use Git/v1 instead"},
		{"GitHub legacy unversioned", githubaccess.Scheme, "github", true, "use Git/v1 instead"},
		{"GitHub camel", githubaccess.Scheme, "gitHub/v1", true, "use Git/v1 instead"},
		{"GitHub camel unversioned", githubaccess.Scheme, "gitHub", true, "use Git/v1 instead"},
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

	t.Run("a type deprecated as a whole has no spelling to match", func(t *testing.T) {
		r := require.New(t)
		scheme := runtime.NewScheme()
		scheme.MustRegister(&rootDeprecated{}, "v1")
		deprecated, hint := isTypeDeprecated(scheme, runtime.NewVersionedType("rootDeprecated", "v1"))
		r.True(deprecated)
		r.Empty(hint)
	})

	t.Run("a type without an embedded schema is not deprecated", func(t *testing.T) {
		r := require.New(t)
		scheme := runtime.NewScheme()
		scheme.MustRegister(&schemaless{}, "v1")
		deprecated, hint := isTypeDeprecated(scheme, runtime.NewVersionedType("schemaless", "v1"))
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

	r.Equal([]deprecatedType{
		{kind: "access", typ: "github", hint: "use Git/v1 instead"},
		{kind: "input", typ: "file", hint: "use file/v1 instead"},
		{kind: "access", typ: "GitHub/v1", hint: "use Git/v1 instead"},
		{kind: "input", typ: "utf8", hint: "use utf8/v1 instead"},
	}, findDeprecatedTypes(accessScheme, inputScheme, spec), "distinct access and input spellings in constructor order")
}

func TestRenderDeprecatedTypeNotice(t *testing.T) {
	t.Run("nothing deprecated prints nothing", func(t *testing.T) {
		r := require.New(t)
		var buf bytes.Buffer
		renderDeprecatedTypeNotice(&buf, nil)
		r.Empty(buf.String())
	})

	t.Run("a blank line and one yellow line per type", func(t *testing.T) {
		r := require.New(t)
		var buf bytes.Buffer
		text.EnableColors()
		t.Cleanup(text.DisableColors)
		renderDeprecatedTypeNotice(&buf, []deprecatedType{
			{kind: "access", typ: "GitHub/v1", hint: "use Git/v1 instead"},
			{kind: "input", typ: "file", hint: "use file/v1 instead"},
			{kind: "access", typ: "git"},
		})
		r.Equal(
			"\n"+text.FgYellow.Sprint("WARNING: access type GitHub/v1 is deprecated, use Git/v1 instead")+"\n"+
				text.FgYellow.Sprint("WARNING: input type file is deprecated, use file/v1 instead")+"\n"+
				text.FgYellow.Sprint("WARNING: access type git is deprecated")+"\n",
			buf.String())
	})

	t.Run("plain text when colors are off", func(t *testing.T) {
		r := require.New(t)
		var buf bytes.Buffer
		text.DisableColors()
		renderDeprecatedTypeNotice(&buf, []deprecatedType{{kind: "input", typ: "utf8", hint: "use utf8/v1 instead"}})
		r.Equal("\nWARNING: input type utf8 is deprecated, use utf8/v1 instead\n", buf.String())
	})
}
