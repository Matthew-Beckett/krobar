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

// Package match compares expected values against simulated objects:
// subset and exact matching plus $-prefixed operators ($regex, $any,
// $absent, $len, $contains, $not, $cel).
package match

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Matthew-Beckett/krobar/internal/ktest/celx"
)

// mode selects the recursive matching semantics.
type mode int

const (
	modeSubset mode = iota
	modeExact
)

// Subset compares an expected subset against a value:
//   - maps: every expected key must exist and match recursively
//   - lists: same length, element-wise subset match
//   - scalars: equal after numeric normalisation (int/int64/float64)
//
// Maps whose keys are all $-prefixed are operator maps; $$ escapes a
// literal key starting with $. It returns one message per mismatch,
// prefixed with the path to the differing value.
func Subset(expected, got interface{}, path string) []string {
	return modeSubset.check(expected, got, path)
}

// Exact compares an expected value requiring equality: maps need
// identical key sets, lists match element-wise at the same length, and
// scalars compare with numeric normalisation. Operators are allowed.
func Exact(expected, got interface{}, path string) []string {
	return modeExact.check(expected, got, path)
}

func (m mode) check(expected, got interface{}, path string) []string {
	if em, ok := expected.(map[string]interface{}); ok {
		opCount := classify(em)
		switch {
		case opCount == len(em) && opCount > 0:
			return m.applyOps(em, got, path)
		case opCount > 0:
			return []string{fmt.Sprintf("%s: cannot mix operators and fields", path)}
		}
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
			ek := unescape(k)
			kp := path + "." + ek
			ev := em[k]
			// A nested operator map containing $absent asserts the key is
			// missing rather than matching its value.
			if om, ok := ev.(map[string]interface{}); ok {
				if oc := classify(om); oc == len(om) && oc > 0 {
					if a, ok := om["$absent"]; ok && a == true {
						if gv, found := gm[ek]; found {
							msgs = append(msgs, fmt.Sprintf("%s: expected absent, got %v", kp, gv))
						}
						continue
					}
				}
			}
			gv, found := gm[ek]
			if !found {
				msgs = append(msgs, fmt.Sprintf("%s: missing key (expected %v)", kp, ev))
				continue
			}
			msgs = append(msgs, m.check(ev, gv, kp)...)
		}
		if m == modeExact {
			wantKeys := make(map[string]bool, len(em))
			for k := range em {
				wantKeys[unescape(k)] = true
			}
			var extra []string
			for gk := range gm {
				if !wantKeys[gk] {
					extra = append(extra, gk)
				}
			}
			sort.Strings(extra)
			for _, gk := range extra {
				msgs = append(msgs, fmt.Sprintf("%s.%s: unexpected key", path, gk))
			}
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
			msgs = append(msgs, m.check(el[i], gl[i], fmt.Sprintf("%s[%d]", path, i))...)
		}
		return msgs
	}
	if !scalarEqual(expected, got) {
		return []string{fmt.Sprintf("%s: expected %v, got %v", path, expected, got)}
	}
	return nil
}

// classify counts operator keys: those starting with $ but not $$, which
// escapes a literal key.
func classify(em map[string]interface{}) (opCount int) {
	for k := range em {
		if strings.HasPrefix(k, "$") && !strings.HasPrefix(k, "$$") {
			opCount++
		}
	}
	return opCount
}

func unescape(k string) string {
	if strings.HasPrefix(k, "$$") {
		return k[1:]
	}
	return k
}

// applyOps evaluates an operator map against a present value.
func (m mode) applyOps(em map[string]interface{}, got interface{}, path string) []string {
	var msgs []string
	keys := make([]string, 0, len(em))
	for k := range em {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ev := em[k]
		switch k {
		case "$regex":
			pat, _ := ev.(string)
			re, err := regexp.Compile(pat)
			gs, isStr := got.(string)
			switch {
			case err != nil:
				msgs = append(msgs, fmt.Sprintf("%s: invalid $regex %q: %s", path, pat, err))
			case !isStr || !re.MatchString(gs):
				msgs = append(msgs, fmt.Sprintf("%s: expected match for /%s/, got %v", path, pat, got))
			}
		case "$any":
			// Presence is the assertion; the value always satisfies it here.
		case "$absent":
			msgs = append(msgs, fmt.Sprintf("%s: expected absent, got %v", path, got))
		case "$len":
			n, _ := toInt(ev)
			if l, ok := length(got); !ok || l != n {
				msgs = append(msgs, fmt.Sprintf("%s: expected length %d, got %v", path, n, got))
			}
		case "$contains":
			list, _ := ev.([]interface{})
			gl, ok := got.([]interface{})
			if !ok {
				msgs = append(msgs, fmt.Sprintf("%s: expected list for $contains, got %v", path, got))
				continue
			}
			used := make([]bool, len(gl))
			for i, elem := range list {
				found := false
				for j := range gl {
					if used[j] {
						continue
					}
					if len(m.check(elem, gl[j], path)) == 0 {
						used[j] = true
						found = true
						break
					}
				}
				if !found {
					msgs = append(msgs, fmt.Sprintf("%s: no element matches $contains[%d] (%v)", path, i, elem))
				}
			}
		case "$not":
			if len(m.check(ev, got, path)) == 0 {
				msgs = append(msgs, fmt.Sprintf("%s: expected not to match %v", path, ev))
			}
		case "$cel":
			expr, _ := ev.(string)
			prog, err := celx.Compile(expr)
			switch {
			case err != nil:
				msgs = append(msgs, fmt.Sprintf("%s: $cel %q: %s", path, expr, err))
			default:
				ok, err := celx.Eval(prog, celx.Vars{Self: got})
				switch {
				case err != nil:
					msgs = append(msgs, fmt.Sprintf("%s: $cel %q: %s", path, expr, err))
				case !ok:
					msgs = append(msgs, fmt.Sprintf("%s: $cel %q was false", path, expr))
				}
			}
		default:
			msgs = append(msgs, fmt.Sprintf("%s: unknown operator %s", path, k))
		}
	}
	return msgs
}

// Validate checks an expected value's operator usage at load time.
// asMapValue tracks whether v appears as a map value ($absent's only
// valid position).
func Validate(expected interface{}, path string) error {
	return validate(expected, path, false)
}

func validate(v interface{}, path string, asMapValue bool) error {
	if em, ok := v.(map[string]interface{}); ok {
		opCount := classify(em)
		if opCount > 0 && opCount < len(em) {
			return fmt.Errorf("%s: cannot mix operators and fields", path)
		}
		keys := make([]string, 0, len(em))
		for k := range em {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if opCount > 0 {
			for _, k := range keys {
				if err := validateOp(k, em[k], path, asMapValue, len(em) > 1); err != nil {
					return err
				}
			}
			return nil
		}
		for _, k := range keys {
			if err := validate(em[k], path+"."+unescape(k), true); err != nil {
				return err
			}
		}
		return nil
	}
	if el, ok := v.([]interface{}); ok {
		for i, e := range el {
			if err := validate(e, fmt.Sprintf("%s[%d]", path, i), false); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOp(op string, v interface{}, path string, asMapValue bool, combined bool) error {
	p := path + "." + op
	switch op {
	case "$regex":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s: must be a string", p)
		}
		if _, err := regexp.Compile(s); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	case "$any":
		if combined {
			return fmt.Errorf("%s: cannot be combined with other operators", p)
		}
		if v != true {
			return fmt.Errorf("%s: must be true", p)
		}
	case "$absent":
		if combined {
			return fmt.Errorf("%s: cannot be combined with other operators", p)
		}
		if v != true {
			return fmt.Errorf("%s: must be true", p)
		}
		if !asMapValue {
			return fmt.Errorf("%s: only valid as a map value", p)
		}
	case "$len":
		if _, ok := toInt(v); !ok {
			return fmt.Errorf("%s: must be an integer", p)
		}
	case "$contains":
		list, ok := v.([]interface{})
		if !ok {
			return fmt.Errorf("%s: must be a list", p)
		}
		for i, e := range list {
			if err := validate(e, fmt.Sprintf("%s[%d]", p, i), false); err != nil {
				return err
			}
		}
	case "$not":
		return validate(v, p, false)
	case "$cel":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s: must be a string", p)
		}
		if _, err := celx.Compile(s); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	default:
		return fmt.Errorf("%s: unknown operator", p)
	}
	return nil
}

// HasOperators reports whether expected contains any operator map.
func HasOperators(expected interface{}) bool {
	if em, ok := expected.(map[string]interface{}); ok {
		opCount := classify(em)
		if opCount > 0 {
			return true
		}
		for _, v := range em {
			if HasOperators(v) {
				return true
			}
		}
		return false
	}
	if el, ok := expected.([]interface{}); ok {
		for _, e := range el {
			if HasOperators(e) {
				return true
			}
		}
	}
	return false
}

func length(v interface{}) (int, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		return len(t), true
	case []interface{}:
		return len(t), true
	case string:
		return utf8.RuneCountInString(t), true
	}
	return 0, false
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
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
