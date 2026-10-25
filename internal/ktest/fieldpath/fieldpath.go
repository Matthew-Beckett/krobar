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

// Package fieldpath parses and applies field selectors like
// `spec.containers[0].image` and `metadata.labels["app/name"]` against
// unstructured values.
package fieldpath

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Segment is one step of a Path: a map key or a list index.
type Segment struct {
	Key     string
	Index   int
	IsIndex bool
}

// Path is a parsed field selector.
type Path []Segment

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Parse parses a field selector. The empty string is the root (zero
// segments). Keys that are not bare identifiers are written in brackets:
// `a["x.y"][2]`; double-quoted keys support \" and \\ escapes.
func Parse(s string) (Path, error) {
	var p Path
	i := 0
	// expectKey is true at the start and right after a '.'.
	expectKey := true
	for i < len(s) {
		switch s[i] {
		case '.':
			if expectKey {
				return nil, fmt.Errorf("%s: empty segment at offset %d", s, i)
			}
			i++
			expectKey = true
		case '[':
			seg, n, err := parseBracket(s, i)
			if err != nil {
				return nil, err
			}
			p = append(p, seg)
			i = n
			expectKey = false
		default:
			if !expectKey {
				return nil, fmt.Errorf("%s: expected '.' or '[' at offset %d", s, i)
			}
			start := i
			for i < len(s) && s[i] != '.' && s[i] != '[' {
				i++
			}
			key := s[start:i]
			if key == "" {
				return nil, fmt.Errorf("%s: empty segment at offset %d", s, start)
			}
			p = append(p, Segment{Key: key})
			expectKey = false
		}
	}
	if expectKey && len(p) > 0 {
		return nil, fmt.Errorf("%s: trailing '.'", s)
	}
	return p, nil
}

// parseBracket parses one `[...]` accessor starting at s[i]=='[' and
// returns the segment and the offset just past ']'.
func parseBracket(s string, i int) (Segment, int, error) {
	j := i + 1
	if j >= len(s) {
		return Segment{}, 0, fmt.Errorf("%s: unterminated '[' at offset %d", s, i)
	}
	if s[j] == '"' {
		j++
		var b strings.Builder
		for j < len(s) {
			c := s[j]
			if c == '\\' && j+1 < len(s) && (s[j+1] == '"' || s[j+1] == '\\') {
				b.WriteByte(s[j+1])
				j += 2
				continue
			}
			if c == '"' {
				break
			}
			b.WriteByte(c)
			j++
		}
		if j >= len(s) || s[j] != '"' {
			return Segment{}, 0, fmt.Errorf("%s: unterminated quote at offset %d", s, i)
		}
		j++
		if j >= len(s) || s[j] != ']' {
			return Segment{}, 0, fmt.Errorf("%s: expected ']' at offset %d", s, j)
		}
		return Segment{Key: b.String()}, j + 1, nil
	}
	start := j
	for j < len(s) && s[j] != ']' {
		j++
	}
	if j >= len(s) {
		return Segment{}, 0, fmt.Errorf("%s: unterminated '[' at offset %d", s, i)
	}
	num := s[start:j]
	idx, err := strconv.Atoi(num)
	if num == "" || err != nil || idx < 0 || strings.TrimLeft(num, "0123456789") != "" {
		return Segment{}, 0, fmt.Errorf("%s: invalid index %q at offset %d", s, num, i)
	}
	return Segment{Index: idx, IsIndex: true}, j + 1, nil
}

// Get walks v and returns the selected value. The second return value is
// false when any step is missing or has the wrong type.
func (p Path) Get(v interface{}) (interface{}, bool) {
	cur := v
	for _, seg := range p {
		if seg.IsIndex {
			list, ok := cur.([]interface{})
			if !ok || seg.Index >= len(list) {
				return nil, false
			}
			cur = list[seg.Index]
			continue
		}
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		val, ok := m[seg.Key]
		if !ok {
			return nil, false
		}
		cur = val
	}
	return cur, true
}

// String returns the canonical selector form: dotted bare keys, bracketed
// quoted keys otherwise.
func (p Path) String() string {
	var b strings.Builder
	for _, seg := range p {
		if seg.IsIndex {
			fmt.Fprintf(&b, "[%d]", seg.Index)
			continue
		}
		if bareKey.MatchString(seg.Key) {
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(seg.Key)
			continue
		}
		b.WriteString(`["`)
		b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(seg.Key))
		b.WriteString(`"]`)
	}
	return b.String()
}
