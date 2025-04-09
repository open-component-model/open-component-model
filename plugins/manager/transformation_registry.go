package manager

import "sync"

type TransformationRegistry struct {
	registry           map[string]map[string]map[string]any
	constructedPlugins map[string]*Plugin
	mu                 sync.Mutex
}

func NewTransformationRegistry() *TransformationRegistry {
	return &TransformationRegistry{
		registry: make(map[string]map[string]map[string]any),
	}
}
