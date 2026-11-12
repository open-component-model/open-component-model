package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReadyState(t *testing.T) {
	tests := []struct {
		name       string
		generation int64
		observed   int64
		conditions []any
		want       objectState
	}{
		{
			name:       "ready and observed",
			generation: 2, observed: 2,
			conditions: []any{map[string]any{"type": "Ready", "status": "True", "reason": "Succeeded"}},
			want:       objectState{ready: true, reason: "Succeeded"},
		},
		{
			name:       "ready for an older generation",
			generation: 3, observed: 2,
			conditions: []any{map[string]any{"type": "Ready", "status": "True", "reason": "Succeeded"}},
			want:       objectState{reason: "Succeeded"},
		},
		{
			name:       "not ready",
			generation: 1, observed: 1,
			conditions: []any{map[string]any{"type": "Ready", "status": "False", "reason": "GetComponentVersionFailed"}},
			want:       objectState{failed: true, reason: "GetComponentVersionFailed"},
		},
		{
			name:       "no ready condition",
			generation: 1, observed: 0,
			conditions: []any{map[string]any{"type": "Other", "status": "True"}},
			want:       objectState{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			u := &unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"generation": tt.generation},
				"status":   map[string]any{"observedGeneration": tt.observed, "conditions": tt.conditions},
			}}
			r.Equal(tt.want, readyState(u))
		})
	}
}
