// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fieldpath

import (
	"testing"
)

func TestParse(t *testing.T) {
	ptr := func(i int) *int { return &i }
	tests := []struct {
		name    string
		path    string
		want    []Segment
		wantErr bool
	}{
		{
			name: "simple single letter path",
			path: "data.A",
			want: []Segment{
				{Name: "data"},
				{Name: "A"},
			},
		},
		{
			name: "simple path",
			path: "spec.containers",
			want: []Segment{
				{Name: "spec"},
				{Name: "containers"},
			},
		},
		{
			name: "path with array",
			path: "spec.containers[0]",
			want: []Segment{
				{Name: "spec"},
				{Name: "containers"},
				{Name: "", Index: ptr(0)},
			},
		},
		{
			name: "path with quoted field",
			path: `spec["my.dotted.field"]`,
			want: []Segment{
				{Name: "spec"},
				{Name: "my.dotted.field"},
			},
		},
		{
			name: "complex path",
			path: `spec["my.field"].items[0]["other.field"]`,
			want: []Segment{
				{Name: "spec"},
				{Name: "my.field"},
				{Name: "items"},
				{Name: "", Index: ptr(0)},
				{Name: "other.field"},
			},
		},
		{
			name: "path with multiple arrays",
			path: `spec.items[0].containers[1]["my.field"][42][""][""]`,
			want: []Segment{
				{Name: "spec"},
				{Name: "items"},
				{Name: "", Index: ptr(0)},
				{Name: "containers"},
				{Name: "", Index: ptr(1)},
				{Name: "my.field"},
				{Name: "", Index: ptr(42)},
				{Name: ""},
				{Name: ""},
			},
		},
		{
			name: "nested arrays",
			path: "'3dmatrix'[0][1][2]",
			want: []Segment{
				{Name: "3dmatrix"},
				{Name: "", Index: ptr(0)},
				{Name: "", Index: ptr(1)},
				{Name: "", Index: ptr(2)},
			},
		},
		{
			name: "starting with key lookup",
			path: `["my.field"].salut`,
			want: []Segment{
				{Name: "my.field"},
				{Name: "salut"},
			},
		},
		{
			name:    "unterminated quote",
			path:    `spec["unterminated`,
			wantErr: true,
		},
		{
			name:    "invalid array index",
			path:    "items[abc]",
			wantErr: true,
		},
		{
			name:    "missing closing bracket",
			path:    `spec["field"`,
			wantErr: true,
		},
		{
			name:    "multiple non close brackets",
			path:    `spec[[[["`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !got.Equals(tt.want) {
				t.Errorf("Parse() = %v, want %v", got, tt.want)
			}
		})
	}
}
