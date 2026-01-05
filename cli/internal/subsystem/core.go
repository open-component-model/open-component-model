package subsystem

import (
	"ocm.software/open-component-model/bindings/go/input/dir"
	"ocm.software/open-component-model/bindings/go/input/file"
	"ocm.software/open-component-model/bindings/go/input/utf8"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func init() {
	RegisterInputMethods()
}

func RegisterInputMethods() {
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(dir.Scheme)
	scheme.MustRegisterScheme(file.Scheme)
	scheme.MustRegisterScheme(utf8.Scheme)

	Register(&Subsystem{
		Name:        "input-method",
		Title:       "Resource/Source Input Methods",
		Description: "Input methods define how content is sourced and ingested into an OCM component version.",
		Scheme:      scheme,
		Guides: []Guide{
			{
				Title:   "Basic Usage in ocm.yaml",
				Summary: "How to use input methods to add resources to a component.",
				Sections: []Section{
					{
						Title:   "Configuration Structure",
						Content: "Specify 'type' and its associated fields within the 'input' block of a resource or source.",
						Example: &Example{
							Caption:  "ocm.yaml with file input",
							Language: "yaml",
							Content: `resources:
  - name: my-resource
    type: plainText
    input:
      type: file
      path: ./data.txt`,
						},
					},
				},
			},
		},
	})
}
