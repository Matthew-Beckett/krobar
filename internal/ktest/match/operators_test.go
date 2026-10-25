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

package match

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExact(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected interface{}
		got      interface{}
		want     []string
	}{
		{
			name:     "map equal",
			expected: map[string]interface{}{"a": 1},
			got:      map[string]interface{}{"a": 1},
		},
		{
			name:     "extra key",
			expected: map[string]interface{}{"a": 1},
			got:      map[string]interface{}{"a": 1, "b": 2},
			want:     []string{"p.b: unexpected key"},
		},
		{
			name:     "missing key",
			expected: map[string]interface{}{"a": 1, "b": 2},
			got:      map[string]interface{}{"a": 1},
			want:     []string{"p.b: missing key (expected 2)"},
		},
		{
			name:     "nested exact",
			expected: map[string]interface{}{"m": map[string]interface{}{"x": 1}},
			got:      map[string]interface{}{"m": map[string]interface{}{"x": 1, "y": 2}},
			want:     []string{"p.m.y: unexpected key"},
		},
		{
			name:     "numeric normalisation",
			expected: map[string]interface{}{"r": int64(3)},
			got:      map[string]interface{}{"r": 3.0},
		},
		{
			name:     "list exact",
			expected: []interface{}{1, "x"},
			got:      []interface{}{1, "x"},
		},
		{
			name:     "list length",
			expected: []interface{}{1},
			got:      []interface{}{1, 2},
			want:     []string{"p: expected 1 element(s), got 2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Exact(tc.expected, tc.got, "p"))
		})
	}
}

func TestOperators(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected interface{}
		got      interface{}
		want     []string
		// wantExact, when non-nil, is used instead of want for Exact.
		wantExact []string
	}{
		{
			name:     "regex match",
			expected: map[string]interface{}{"image": map[string]interface{}{"$regex": `^nginx`}},
			got:      map[string]interface{}{"image": "nginx:1.27"},
		},
		{
			name:     "regex miss",
			expected: map[string]interface{}{"image": map[string]interface{}{"$regex": `^alpine`}},
			got:      map[string]interface{}{"image": "nginx"},
			want:     []string{`p.image: expected match for /^alpine/, got nginx`},
		},
		{
			name:     "regex on non-string",
			expected: map[string]interface{}{"image": map[string]interface{}{"$regex": `x`}},
			got:      map[string]interface{}{"image": 3},
			want:     []string{`p.image: expected match for /x/, got 3`},
		},
		{
			name:     "any present",
			expected: map[string]interface{}{"status": map[string]interface{}{"$any": true}},
			got:      map[string]interface{}{"status": map[string]interface{}{"x": 1}},
		},
		{
			name:     "any missing key",
			expected: map[string]interface{}{"status": map[string]interface{}{"$any": true}},
			got:      map[string]interface{}{},
			want:     []string{`p.status: missing key (expected map[$any:true])`},
		},
		{
			name:     "absent satisfied",
			expected: map[string]interface{}{"status": map[string]interface{}{"$absent": true}},
			got:      map[string]interface{}{"spec": 1},
			// Exact also flags the extra "spec" key.
			wantExact: []string{"p.spec: unexpected key"},
		},
		{
			name:     "absent violated",
			expected: map[string]interface{}{"status": map[string]interface{}{"$absent": true}},
			got:      map[string]interface{}{"status": "x"},
			want:     []string{"p.status: expected absent, got x"},
		},
		{
			name:     "len list",
			expected: map[string]interface{}{"items": map[string]interface{}{"$len": 2}},
			got:      map[string]interface{}{"items": []interface{}{1, 2}},
		},
		{
			name:     "len string",
			expected: map[string]interface{}{"s": map[string]interface{}{"$len": 3}},
			got:      map[string]interface{}{"s": "abc"},
		},
		{
			name:     "len counts runes not bytes",
			expected: map[string]interface{}{"s": map[string]interface{}{"$len": 3}},
			got:      map[string]interface{}{"s": "\u65e5\u672c\u8a9e"},
		},
		{
			name:     "len rune mismatch",
			expected: map[string]interface{}{"s": map[string]interface{}{"$len": 9}},
			got:      map[string]interface{}{"s": "\u65e5\u672c\u8a9e"},
			want:     []string{"p.s: expected length 9, got \u65e5\u672c\u8a9e"},
		},
		{
			name:     "len mismatch",
			expected: map[string]interface{}{"items": map[string]interface{}{"$len": 3}},
			got:      map[string]interface{}{"items": []interface{}{1}},
			want:     []string{"p.items: expected length 3, got [1]"},
		},
		{
			name:     "contains ok distinct",
			expected: map[string]interface{}{"env": map[string]interface{}{"$contains": []interface{}{"a", "b"}}},
			got:      map[string]interface{}{"env": []interface{}{"x", "a", "b"}},
		},
		{
			name:     "contains requires distinct elements",
			expected: map[string]interface{}{"env": map[string]interface{}{"$contains": []interface{}{"a", "a"}}},
			got:      map[string]interface{}{"env": []interface{}{"a", "b"}},
			want:     []string{"p.env: no element matches $contains[1] (a)"},
		},
		{
			name:     "contains subset element",
			expected: map[string]interface{}{"cs": map[string]interface{}{"$contains": []interface{}{map[string]interface{}{"name": "web"}}}},
			got:      map[string]interface{}{"cs": []interface{}{map[string]interface{}{"name": "web", "image": "nginx"}}},
			// Exact element match requires identical key sets.
			wantExact: []string{"p.cs: no element matches $contains[0] (map[name:web])"},
		},
		{
			name:     "not satisfied",
			expected: map[string]interface{}{"img": map[string]interface{}{"$not": "alpine"}},
			got:      map[string]interface{}{"img": "nginx"},
		},
		{
			name:     "not violated",
			expected: map[string]interface{}{"img": map[string]interface{}{"$not": "nginx"}},
			got:      map[string]interface{}{"img": "nginx"},
			want:     []string{"p.img: expected not to match nginx"},
		},
		{
			name:     "cel true",
			expected: map[string]interface{}{"r": map[string]interface{}{"$cel": `self > 1`}},
			got:      map[string]interface{}{"r": 3},
		},
		{
			name:     "cel false",
			expected: map[string]interface{}{"r": map[string]interface{}{"$cel": `self > 5`}},
			got:      map[string]interface{}{"r": 3},
			want:     []string{`p.r: $cel "self > 5" was false`},
		},
		{
			name:     "cel eval error",
			expected: map[string]interface{}{"r": map[string]interface{}{"$cel": `self.list[9] > 0`}},
			got:      map[string]interface{}{"r": map[string]interface{}{"list": []interface{}{}}},
			want:     []string{`p.r: $cel "self.list[9] > 0": index out of bounds: 9`},
		},
		{
			name:     "multiple operators all must hold",
			expected: map[string]interface{}{"img": map[string]interface{}{"$regex": `nginx`, "$not": "nginx:old"}},
			got:      map[string]interface{}{"img": "nginx:old"},
			want:     []string{"p.img: expected not to match nginx:old"},
		},
		{
			name:     "operators nested in lists",
			expected: map[string]interface{}{"cs": []interface{}{map[string]interface{}{"img": map[string]interface{}{"$regex": `nginx`}}}},
			got:      map[string]interface{}{"cs": []interface{}{map[string]interface{}{"img": "apache"}}},
			want:     []string{`p.cs[0].img: expected match for /nginx/, got apache`},
		},
		{
			name:     "escaped dollar key",
			expected: map[string]interface{}{"$$weird": 1},
			got:      map[string]interface{}{"$weird": 1},
		},
		{
			name:     "mixed operators and fields",
			expected: map[string]interface{}{"img": map[string]interface{}{"$regex": "x", "tag": "y"}},
			got:      map[string]interface{}{"img": map[string]interface{}{"tag": "y"}},
			want:     []string{"p.img: cannot mix operators and fields"},
		},
		{
			name:     "unknown operator",
			expected: map[string]interface{}{"img": map[string]interface{}{"$bogus": 1}},
			got:      map[string]interface{}{"img": "x"},
			want:     []string{"p.img: unknown operator $bogus"},
		},
	} {
		t.Run(tc.name+" subset", func(t *testing.T) {
			assert.Equal(t, tc.want, Subset(tc.expected, tc.got, "p"))
		})
		t.Run(tc.name+" exact", func(t *testing.T) {
			want := tc.want
			if tc.wantExact != nil {
				want = tc.wantExact
			}
			assert.Equal(t, want, Exact(tc.expected, tc.got, "p"))
		})
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected interface{}
		wantErr  string
	}{
		{"plain", map[string]interface{}{"a": 1}, ""},
		{"good ops", map[string]interface{}{"a": map[string]interface{}{
			"$regex": "x", "$len": 2,
			"$contains": []interface{}{1}, "$not": "y", "$cel": "self > 0",
		}}, ""},
		{"absent as map value", map[string]interface{}{"a": map[string]interface{}{"$absent": true}}, ""},
		{"absent at root", map[string]interface{}{"$absent": true}, "$absent"},
		{"absent in list", []interface{}{map[string]interface{}{"$absent": true}}, "$absent"},
		{"mixed", map[string]interface{}{"a": map[string]interface{}{"$regex": "x", "b": 1}}, "cannot mix"},
		{"unknown op", map[string]interface{}{"a": map[string]interface{}{"$zzz": 1}}, "unknown operator"},
		{"any not true", map[string]interface{}{"a": map[string]interface{}{"$any": "yes"}}, "must be true"},
		{"absent not true", map[string]interface{}{"a": map[string]interface{}{"$absent": 1}}, "must be true"},
		{"regex not string", map[string]interface{}{"a": map[string]interface{}{"$regex": 3}}, "must be a string"},
		{"regex bad", map[string]interface{}{"a": map[string]interface{}{"$regex": "([bad"}}, "$regex"},
		{"len not int", map[string]interface{}{"a": map[string]interface{}{"$len": "x"}}, "must be an integer"},
		{"contains not list", map[string]interface{}{"a": map[string]interface{}{"$contains": "x"}}, "must be a list"},
		{"cel not string", map[string]interface{}{"a": map[string]interface{}{"$cel": 1}}, "must be a string"},
		{"cel bad", map[string]interface{}{"a": map[string]interface{}{"$cel": "self."}}, "$cel"},
		{"cel nested in not", map[string]interface{}{"a": map[string]interface{}{"$not": map[string]interface{}{"$bogus": 1}}}, "unknown operator"},
		{"absent combined", map[string]interface{}{"a": map[string]interface{}{"$absent": true, "$regex": "x"}}, "$absent: cannot be combined"},
		{"any combined with absent", map[string]interface{}{"a": map[string]interface{}{"$any": true, "$absent": true}}, "cannot be combined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.expected, "p")
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestHasOperators(t *testing.T) {
	assert.False(t, HasOperators(map[string]interface{}{"a": map[string]interface{}{"b": 1}}))
	assert.False(t, HasOperators(nil))
	assert.True(t, HasOperators(map[string]interface{}{"a": map[string]interface{}{"$regex": "x"}}))
	assert.True(t, HasOperators([]interface{}{map[string]interface{}{"$len": 1}}))
}
