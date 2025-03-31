/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestDeepCopyJSON(t *testing.T) {
	src := map[string]interface{}{
		"a": nil,
		"b": int64(123),
		"c": map[string]interface{}{
			"a": "b",
		},
		"d": []interface{}{
			int64(1), int64(2),
		},
		"e": "estr",
		"f": true,
		"g": json.Number("123"),
	}
	deepCopy := runtime.DeepCopyJSON(src)
	assert.Equal(t, src, deepCopy)
}
