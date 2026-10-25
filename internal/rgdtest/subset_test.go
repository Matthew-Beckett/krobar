// Copyright 2026 The Krobar Authors.
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

package rgdtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchSubset(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected interface{}
		got      interface{}
		want     []string
	}{
		{
			name:     "scalar equal",
			expected: "a",
			got:      "a",
		},
		{
			name:     "scalar mismatch",
			expected: "a",
			got:      "b",
			want:     []string{"p: expected a, got b"},
		},
		{
			name:     "numeric normalisation int64 vs float64",
			expected: int64(3),
			got:      float64(3),
		},
		{
			name:     "numeric normalisation float vs int",
			expected: float64(2),
			got:      int(2),
		},
		{
			name:     "numeric mismatch",
			expected: int64(3),
			got:      int64(4),
			want:     []string{"p: expected 3, got 4"},
		},
		{
			name:     "map subset",
			expected: map[string]interface{}{"a": map[string]interface{}{"b": 1}},
			got:      map[string]interface{}{"a": map[string]interface{}{"b": 1, "c": 2}},
		},
		{
			name:     "map missing key",
			expected: map[string]interface{}{"a": 1},
			got:      map[string]interface{}{},
			want:     []string{"p.a: missing key (expected 1)"},
		},
		{
			name:     "nested map mismatch carries path",
			expected: map[string]interface{}{"spec": map[string]interface{}{"replicas": 3}},
			got:      map[string]interface{}{"spec": map[string]interface{}{"replicas": 1}},
			want:     []string{"p.spec.replicas: expected 3, got 1"},
		},
		{
			name:     "list element-wise",
			expected: []interface{}{map[string]interface{}{"x": 1}, "y"},
			got:      []interface{}{map[string]interface{}{"x": 1, "z": 2}, "y"},
		},
		{
			name:     "list length mismatch",
			expected: []interface{}{1, 2},
			got:      []interface{}{1},
			want:     []string{"p: expected 2 element(s), got 1"},
		},
		{
			name:     "list element mismatch carries index",
			expected: []interface{}{1, 2},
			got:      []interface{}{1, 9},
			want:     []string{"p[1]: expected 2, got 9"},
		},
		{
			name:     "expected map got scalar",
			expected: map[string]interface{}{"a": 1},
			got:      "x",
			want:     []string{"p: expected object, got x"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, MatchSubset(tc.expected, tc.got, "p"))
		})
	}
}
