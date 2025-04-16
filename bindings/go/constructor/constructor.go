package constructor

type ComponentConstructor struct {
	Components []Component
}

type Component struct {
	Name     string
	Version  string
	Provider Provider
}

type Provider struct {
	Name string
}
