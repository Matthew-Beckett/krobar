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

// Package fmtcmd implements `krobar fmt`: canonical, comment-preserving YAML
// formatting for ResourceGraphDefinitions and other manifests.
package fmtcmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Canonical key orders applied to ResourceGraphDefinition mappings. Unlisted
// keys keep their original relative order after the listed ones.
var (
	topLevelOrder = []string{"apiVersion", "kind", "metadata", "spec", "status"}
	metadataOrder = []string{"name", "namespace", "labels", "annotations"}
	specOrder     = []string{"schema", "resources"}
	schemaOrder   = []string{"apiVersion", "kind", "group", "scope", "spec", "types", "status", "additionalPrinterColumns"}
	resourceOrder = []string{"id", "readyWhen", "includeWhen", "forEach", "externalRef", "template"}
	templateOrder = []string{"apiVersion", "kind", "metadata", "spec"}
)

// Format normalises YAML input: 2-space indent, preserved comments and scalar
// styles, multi-document input preserved. Documents of kind
// ResourceGraphDefinition additionally get canonical key ordering.
func Format(data []byte) ([]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)

	wrote := false
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		if len(node.Content) == 0 {
			continue // empty document
		}
		normalizeBlockScalars(&node)
		if docKind(&node) == "ResourceGraphDefinition" {
			formatRGD(&node)
		}
		if err := enc.Encode(&node); err != nil {
			return nil, err
		}
		wrote = true
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if !wrote {
		return nil, fmt.Errorf("no YAML documents found")
	}
	return out.Bytes(), nil
}

// normalizeBlockScalars works around a go-yaml emitter bug: folded (`>`)
// scalars whose value contains newlines gain a newline on every round trip.
// Rewriting them as double-quoted scalars keeps the value byte-exact and
// idempotent. Literal (`|`) scalars round-trip correctly.
func normalizeBlockScalars(node *yaml.Node) {
	if node.Kind == yaml.ScalarNode &&
		node.Style == yaml.FoldedStyle &&
		strings.Contains(node.Value, "\n") {
		node.Style = yaml.DoubleQuotedStyle
	}
	for _, c := range node.Content {
		normalizeBlockScalars(c)
	}
}

func docKind(node *yaml.Node) string {
	m := unwrap(node)
	if m == nil {
		return ""
	}
	return scalarValue(m, "kind")
}

// unwrap returns the mapping node at the top of a document node.
func unwrap(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

// scalarValue returns the scalar value of key in mapping m, or "".
func scalarValue(m *yaml.Node, key string) string {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1].Value
		}
	}
	return ""
}

// childMap returns the mapping child stored under key, or nil.
func childMap(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i+1].Kind == yaml.MappingNode {
			return m.Content[i+1]
		}
	}
	return nil
}

// childSeq returns the sequence child stored under key, or nil.
func childSeq(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i+1].Kind == yaml.SequenceNode {
			return m.Content[i+1]
		}
	}
	return nil
}

// reorder reorders the key/value pairs of a mapping node so keys in order
// come first (in that order); remaining keys keep their relative order.
func reorder(m *yaml.Node, order []string) {
	if m == nil || len(m.Content) == 0 {
		return
	}
	rank := make(map[string]int, len(order))
	for i, k := range order {
		rank[k] = i
	}
	type pair struct{ k, v *yaml.Node }
	var head, tail []pair
	for i := 0; i+1 < len(m.Content); i += 2 {
		p := pair{m.Content[i], m.Content[i+1]}
		if _, ok := rank[p.k.Value]; ok {
			head = append(head, p)
		} else {
			tail = append(tail, p)
		}
	}
	slices.SortStableFunc(head, func(a, b pair) int {
		return rank[a.k.Value] - rank[b.k.Value]
	})
	content := make([]*yaml.Node, 0, len(m.Content))
	for _, p := range head {
		content = append(content, p.k, p.v)
	}
	for _, p := range tail {
		content = append(content, p.k, p.v)
	}
	m.Content = content
}

// formatRGD applies canonical key ordering to a ResourceGraphDefinition doc.
func formatRGD(doc *yaml.Node) {
	m := unwrap(doc)
	if m == nil {
		return
	}
	reorder(m, topLevelOrder)
	reorder(childMap(m, "metadata"), metadataOrder)
	spec := childMap(m, "spec")
	if spec == nil {
		return
	}
	reorder(spec, specOrder)
	reorder(childMap(spec, "schema"), schemaOrder)
	if res := childSeq(spec, "resources"); res != nil {
		for _, item := range res.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			reorder(item, resourceOrder)
			reorder(childMap(item, "template"), templateOrder)
		}
	}
}
