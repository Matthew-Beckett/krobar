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

package fmtcmd

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"
)

// kroExamplesDir resolves the examples tree inside the cached kro module.
// The test is skipped when the module cannot be located.
func kroExamplesDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}",
		"github.com/kubernetes-sigs/kro").Output() // #nosec G204 -- fixed args
	dir := filepath.Join(strings.TrimSpace(string(out)), "examples")
	if err != nil || !dirExists(dir) {
		t.Skipf("kro module examples not resolvable: %v", err)
	}
	return dir
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// decodeAll decodes every document for semantic comparison. It decodes into
// yaml.Node (which tolerates duplicate keys, unlike decoding into any) and
// converts to plain values.
func decodeAll(t *testing.T, data []byte) []any {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		docs = append(docs, nodeToAny(&n))
	}
	return docs
}

func nodeToAny(n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
		}
		return nodeToAny(n.Content[0])
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = nodeToAny(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		var s []any
		for _, c := range n.Content {
			s = append(s, nodeToAny(c))
		}
		return s
	case yaml.AliasNode:
		return nodeToAny(n.Alias)
	default:
		var v any
		if err := n.Decode(&v); err != nil {
			return n.Value
		}
		return v
	}
}

func isRGD(data []byte) bool {
	var meta struct {
		Kind string `yaml:"kind"`
	}
	return yaml.Unmarshal(data, &meta) == nil && meta.Kind == "ResourceGraphDefinition"
}

// TestIdempotentAndSemantic runs over every RGD example shipped with kro:
// fmt(fmt(x)) == fmt(x) and the formatted output decodes to an identical
// document tree.
func TestIdempotentAndSemantic(t *testing.T) {
	count := 0
	err := filepath.WalkDir(kroExamplesDir(t), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (filepath.Ext(p) != ".yaml" && filepath.Ext(p) != ".yml") {
			return nil
		}
		data, err := os.ReadFile(p) // #nosec G304 G122 -- test fixture paths from filepath walk
		if err != nil {
			return err
		}
		if !isRGD(data) {
			return nil
		}
		count++
		once, err := Format(data)
		if err != nil {
			t.Errorf("%s: format failed: %v", p, err)
			return nil
		}
		twice, err := Format(once)
		if err != nil {
			t.Errorf("%s: second format failed: %v", p, err)
			return nil
		}
		assert.Equal(t, string(once), string(twice), "%s not idempotent", p)

		origDocs := decodeAll(t, data)
		formattedDocs := decodeAll(t, once)
		require.Equal(t, len(origDocs), len(formattedDocs), "%s doc count changed", p)
		for i := range origDocs {
			assert.True(t, reflect.DeepEqual(origDocs[i], formattedDocs[i]),
				"%s doc %d semantically changed", p, i)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, count, 3, "expected several RGD examples, got %d", count)
}

func TestCommentPreservation(t *testing.T) {
	in := `# leading comment
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: x # trailing comment
spec:
  schema: {}
`
	out, err := Format([]byte(in))
	require.NoError(t, err)
	assert.Contains(t, string(out), "# leading comment")
	assert.Contains(t, string(out), "# trailing comment")
}

func TestOrdering(t *testing.T) {
	in := `kind: ResourceGraphDefinition
spec:
  resources:
  - template:
      spec:
        replicas: 1
      kind: Deployment
      apiVersion: apps/v1
    id: deployment
  schema:
    kind: WebApp
    apiVersion: v1alpha1
apiVersion: kro.run/v1alpha1
metadata:
  annotations: {}
  name: x
`
	out, err := Format([]byte(in))
	require.NoError(t, err)
	s := string(out)

	// top-level order
	assert.Less(t, strings.Index(s, "apiVersion:"), strings.Index(s, "\nkind:"))
	assert.Less(t, strings.Index(s, "\nkind:"), strings.Index(s, "\nmetadata:"))
	assert.Less(t, strings.Index(s, "\nmetadata:"), strings.Index(s, "\nspec:"))
	// spec: schema before resources
	assert.Less(t, strings.Index(s, "  schema:"), strings.Index(s, "  resources:"))
	// schema: apiVersion before kind
	assert.Less(t, strings.Index(s, "    apiVersion:"), strings.Index(s, "    kind:"))
	// resource item: id before template; template: apiVersion, kind, spec
	assert.Less(t, strings.Index(s, "    id:"), strings.Index(s, "    template:"))
	assert.Less(t, strings.Index(s, "      apiVersion:"), strings.Index(s, "      kind:"))
	assert.Less(t, strings.Index(s, "      kind:"), strings.Index(s, "      spec:"))
	// metadata: name before annotations
	assert.Less(t, strings.Index(s, "  name:"), strings.Index(s, "  annotations:"))
}

func TestCELExpressionsUnwrapped(t *testing.T) {
	long := "${schema.spec.someVeryLongFieldName + deployment.status.conditions[?(@.type=='Ready')].status + service.status.loadBalancer.ingress[0].hostname}"
	in := "kind: ResourceGraphDefinition\nspec:\n  resources:\n  - id: x\n    readyWhen:\n    - " + long + "\n    template:\n      apiVersion: v1\n      kind: ConfigMap\n      metadata:\n        name: ${schema.spec.name}\n"
	out, err := Format([]byte(in))
	require.NoError(t, err)
	assert.Contains(t, string(out), long)
	assert.Contains(t, string(out), "${schema.spec.name}")
}

func TestNonRGDOnlyNormalised(t *testing.T) {
	in := "kind: ConfigMap\nmetadata:\n    name:    x\n"
	out, err := Format([]byte(in))
	require.NoError(t, err)
	assert.Contains(t, string(out), "metadata:\n  name: x")
	// kind stays first since it already was
	assert.True(t, strings.HasPrefix(string(out), "kind: ConfigMap"))
}

func TestMultiDoc(t *testing.T) {
	in := "kind: ConfigMap\nmetadata:\n  name: a\n---\nkind: ConfigMap\nmetadata:\n  name: b\n"
	out, err := Format([]byte(in))
	require.NoError(t, err)
	docs := decodeAll(t, out)
	require.Len(t, docs, 2)
}
