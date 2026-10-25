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
	"fmt"
	"sort"
)

// MatchSubset compares an expected subset against a value:
//   - maps: every expected key must exist and match recursively
//   - lists: same length, element-wise subset match
//   - scalars: equal after numeric normalisation (int/int64/float64)
//
// It returns one message per mismatch, prefixed with the path to the
// differing value.
func MatchSubset(expected, got interface{}, path string) []string {
	if em, ok := expected.(map[string]interface{}); ok {
		gm, ok := got.(map[string]interface{})
		if !ok {
			return []string{fmt.Sprintf("%s: expected object, got %v", path, got)}
		}
		var msgs []string
		keys := make([]string, 0, len(em))
		for k := range em {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			gv, found := gm[k]
			if !found {
				msgs = append(msgs, fmt.Sprintf("%s.%s: missing key (expected %v)", path, k, em[k]))
				continue
			}
			msgs = append(msgs, MatchSubset(em[k], gv, path+"."+k)...)
		}
		return msgs
	}
	if el, ok := expected.([]interface{}); ok {
		gl, ok := got.([]interface{})
		if !ok {
			return []string{fmt.Sprintf("%s: expected list, got %v", path, got)}
		}
		if len(gl) != len(el) {
			return []string{fmt.Sprintf("%s: expected %d element(s), got %d", path, len(el), len(gl))}
		}
		var msgs []string
		for i := range el {
			msgs = append(msgs, MatchSubset(el[i], gl[i], fmt.Sprintf("%s[%d]", path, i))...)
		}
		return msgs
	}
	if !scalarEqual(expected, got) {
		return []string{fmt.Sprintf("%s: expected %v, got %v", path, expected, got)}
	}
	return nil
}

// scalarEqual compares scalars, normalising all JSON/YAML numeric types to
// float64 so an integer literal matches either decoding.
func scalarEqual(expected, got interface{}) bool {
	if ef, ok := num(expected); ok {
		gf, ok := num(got)
		return ok && ef == gf
	}
	return expected == got
}

func num(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
