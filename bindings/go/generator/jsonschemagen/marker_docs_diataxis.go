package jsonschemagen

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime/docs"
	"ocm.software/open-component-model/bindings/go/runtime/docs/diataxis"
)

type DocsMarker struct {
	Kind     string   // tutorial | howto | explanation
	Path     string   // docs/...
	Audience []string // user | operator | author | implementer
}

func ApplyDocsMarkers(
	sch *JSONSchemaDraft202012,
	basePath string,
	markers []DocsMarker,
) error {
	if sch == nil || len(markers) == 0 {
		return nil
	}

	if sch.XDocs == nil {
		sch.XDocs = &docs.XDocs{}
	}

	for _, m := range markers {
		if m.Path == "" {
			continue
		}

		// Resolve and embed documentation content
		content, err := loadDocContent(basePath, m.Path)
		if err != nil {
			return err
		}

		ref := diataxis.DocRef{
			Content: content,
		}

		if len(m.Audience) > 0 {
			ref.Audience = m.Audience
		}

		switch m.Kind {
		case "tutorial":
			sch.XDocs.Diataxis.Tutorial =
				append(sch.XDocs.Diataxis.Tutorial, ref)

		case "howto":
			sch.XDocs.Diataxis.HowTo =
				append(sch.XDocs.Diataxis.HowTo, ref)

		case "explanation":
			sch.XDocs.Diataxis.Explanation =
				append(sch.XDocs.Diataxis.Explanation, ref)

		default:
			// Unknown kind → ignore silently
			// (keeps generator forward-compatible)
		}
	}

	return nil
}

const DocsDiataxisBase = BaseMarker + ":docs:diataxis"

// ExtractDocsMarkers extracts documentation markers from the given comment groups.
// It supports per-doc audience using the compact syntax:
//
//	+ocm:jsonschema-gen:docs:diataxis:concept=path,audience=user,audience=implementer
func ExtractDocsMarkers(cgs ...*ast.CommentGroup) []DocsMarker {
	var out []DocsMarker

	for _, cg := range cgs {
		if cg == nil {
			continue
		}

		// Reuse the existing marker parser
		raw := ExtractMarkers(cg, DocsDiataxisBase)
		if len(raw) == 0 {
			continue
		}

		// Parse audience (shared for this marker line)
		var audience []string
		if audRaw, ok := raw["audience"]; ok {
			for _, a := range strings.Split(audRaw, ",") {
				a = strings.TrimSpace(a)
				if a != "" {
					audience = append(audience, a)
				}
			}
		}

		// Each non-audience key represents one docRef
		for key, val := range raw {
			if key == "audience" {
				continue
			}

			if val == "" {
				continue
			}

			out = append(out, DocsMarker{
				Kind:     key, // concept | howto | explanation
				Path:     val, // docs/...
				Audience: audience,
			})
		}
	}

	return out
}

func loadDocContent(basePath, rel string) (string, error) {
	p := rel
	if !filepath.IsAbs(rel) {
		p = filepath.Join(basePath, rel)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
