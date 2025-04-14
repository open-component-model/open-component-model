package manager

type TransformationRegistry struct {
	registry map[string]map[string]map[string]any
}

func NewTransformationRegistry() *TransformationRegistry {
	return &TransformationRegistry{
		registry: make(map[string]map[string]map[string]any),
	}
}
