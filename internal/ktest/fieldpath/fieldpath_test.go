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

package fieldpath

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Path
	}{
		{"", nil},
		{"a", Path{{Key: "a"}}},
		{"spec.containers[0].image", Path{
			{Key: "spec"}, {Key: "containers"}, {Index: 0, IsIndex: true}, {Key: "image"},
		}},
		{`metadata.labels["app.kubernetes.io/name"]`, Path{
			{Key: "metadata"}, {Key: "labels"}, {Key: "app.kubernetes.io/name"},
		}},
		{`a["x.y"][2]`, Path{
			{Key: "a"}, {Key: "x.y"}, {Index: 2, IsIndex: true},
		}},
		{`a["q\"uote"]`, Path{{Key: "a"}, {Key: `q"uote`}}},
		{`a["back\\slash"]`, Path{{Key: "a"}, {Key: `back\slash`}}},
		{`["lead"].x`, Path{{Key: "lead"}, {Key: "x"}}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Parse(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"a..b", ".a", "a.", "a[", `a["x`, `a["x"`, `a["x"y]`, "a[-1]",
		"a[x]", "a[1x]", "a[+1]", "a[]", "a[0]b",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := Parse(in)
			assert.Error(t, err)
		})
	}
}

func TestGet(t *testing.T) {
	v := map[string]interface{}{
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{"image": "nginx"},
			},
		},
		"weird.key": "x",
		"scalar":    3.0,
	}
	get := func(t *testing.T, path string) (interface{}, bool) {
		t.Helper()
		p, err := Parse(path)
		require.NoError(t, err)
		return p.Get(v)
	}
	got, ok := get(t, "spec.containers[0].image")
	assert.True(t, ok)
	assert.Equal(t, "nginx", got)

	got, ok = get(t, `["weird.key"]`)
	assert.True(t, ok)
	assert.Equal(t, "x", got)

	// Root returns the value itself.
	p, err := Parse("")
	require.NoError(t, err)
	got, ok = p.Get(v)
	assert.True(t, ok)
	assert.Equal(t, v, got)

	for _, path := range []string{
		"spec.missing", "spec.containers[5]", "spec.containers[0].image.x",
		"scalar.y", "spec.containers.x", "spec[0]",
	} {
		_, ok := get(t, path)
		assert.False(t, ok, path)
	}
}

func TestStringRoundTrip(t *testing.T) {
	for _, in := range []string{
		"",
		"a.b.c",
		"spec.containers[0].image",
		`metadata.labels["app.kubernetes.io/name"]`,
		`a["x.y"][2]`,
		`a["q\"uote"]`,
	} {
		p, err := Parse(in)
		require.NoError(t, err)
		s := p.String()
		back, err := Parse(s)
		require.NoError(t, err)
		assert.Equal(t, p, back)
		assert.Equal(t, s, back.String())
	}
	assert.Equal(t, `metadata.labels["app.kubernetes.io/name"]`,
		Path{{Key: "metadata"}, {Key: "labels"}, {Key: "app.kubernetes.io/name"}}.String())
}
