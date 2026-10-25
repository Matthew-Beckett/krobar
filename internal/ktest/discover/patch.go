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

package discover

// mergePatch applies an RFC 7386 JSON merge patch to base: maps merge
// recursively, a null patch value deletes the key, everything else
// replaces. Neither input is modified.
func mergePatch(base, patch map[string]interface{}) map[string]interface{} {
	out := deepCopyMap(base)
	for k, pv := range patch {
		if pv == nil {
			delete(out, k)
			continue
		}
		if pm, ok := pv.(map[string]interface{}); ok {
			bm, _ := out[k].(map[string]interface{})
			out[k] = mergePatch(bm, pm)
			continue
		}
		out[k] = deepCopyValue(pv)
	}
	return out
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = deepCopyValue(e)
		}
		return out
	default:
		return v
	}
}
