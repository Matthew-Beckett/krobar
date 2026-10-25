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

package snapshot

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/Matthew-Beckett/krobar/internal/ktest/fieldpath"
	"github.com/Matthew-Beckett/krobar/internal/simulate"
)

func obj(extra map[string]interface{}) *unstructured.Unstructured {
	o := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "cm1"},
	}
	for k, v := range extra {
		o[k] = v
	}
	return &unstructured.Unstructured{Object: o}
}

func res(status map[string]interface{}, nodes ...simulate.NodeResult) *simulate.Result {
	inst := map[string]interface{}{
		"apiVersion": "kro.run/v1alpha1", "kind": "Mini",
		"metadata": map[string]interface{}{"name": "i"},
	}
	if status != nil {
		inst["status"] = status
	}
	return &simulate.Result{
		Instance: &unstructured.Unstructured{Object: inst},
		Nodes:    nodes,
	}
}

func node(id string, state simulate.State, objs ...*unstructured.Unstructured) simulate.NodeResult {
	return simulate.NodeResult{ID: id, State: state, Objects: objs}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	return dir
}

const cmDoc = `apiVersion: v1
kind: ConfigMap
metadata: {name: cm1}
`

// containsAll asserts that every want substring appears in some message.
func containsAll(t *testing.T, msgs []string, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, m := range msgs {
			if strings.Contains(m, w) {
				found = true
				break
			}
		}
		assert.True(t, found, "no message contains %q; got %v", w, msgs)
	}
}

func TestEvalMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "expected")
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))

	fails, _ := Eval(dir, Settings{Mode: ModeStrict}, r, false)
	containsAll(t, fails, "snapshot directory "+dir+" does not exist")

	fails, _ = Eval(dir, Settings{Mode: ModePartial}, r, false)
	assert.Empty(t, fails)

	fails, _ = Eval(dir, Settings{Mode: ModeOff}, r, false)
	assert.Empty(t, fails)
}

func TestEvalStrictMissingNodeFile(t *testing.T) {
	dir := writeFiles(t, nil)
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))

	fails, _ := Eval(dir, Settings{Mode: ModeStrict}, r, false)
	containsAll(t, fails,
		`snapshot: node "cm" rendered 1 object(s) but has no snapshot file`,
		`snapshot _states.yaml: missing entry for node "cm"`)

	// Partial mode skips missing files.
	fails, _ = Eval(dir, Settings{Mode: ModePartial}, r, false)
	assert.Empty(t, fails)
}

func TestEvalFileForAbsentNode(t *testing.T) {
	dir := writeFiles(t, map[string]string{"gone.yaml": cmDoc})
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))

	fails, _ := Eval(dir, Settings{Mode: ModeStrict}, r, false)
	containsAll(t, fails, `snapshot: node "gone" has a snapshot file but the result has no such node`)
}

func TestEvalFileButNoObjects(t *testing.T) {
	dir := writeFiles(t, map[string]string{"svc.yaml": cmDoc})
	r := res(nil, node("svc", simulate.StatePending))

	for _, mode := range []string{ModeStrict, ModePartial} {
		fails, _ := Eval(dir, Settings{Mode: mode}, r, false)
		containsAll(t, fails, `snapshot: node "svc" has a snapshot file but rendered no objects`)
	}
}

func TestEvalEmptyFiles(t *testing.T) {
	// An empty file for a node that rendered objects is a count mismatch.
	dir := writeFiles(t, map[string]string{"cm.yaml": ""})
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))
	fails, _ := Eval(dir, Settings{Mode: ModeStrict}, r, false)
	containsAll(t, fails, "snapshot cm.yaml: expected 0 object(s), got 1")

	// An empty file for an empty/absent node is fine (partial isolates it).
	dir = writeFiles(t, map[string]string{"svc.yaml": "", "gone.yaml": ""})
	r = res(nil, node("svc", simulate.StatePending))
	fails, _ = Eval(dir, Settings{Mode: ModePartial}, r, false)
	assert.Empty(t, fails)
}

func TestEvalDocMismatchAndMatch(t *testing.T) {
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))

	dir := writeFiles(t, map[string]string{"cm.yaml": `apiVersion: v1
kind: ConfigMap
metadata: {name: other}
`})
	fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
	require.Len(t, fails, 1)
	assert.Contains(t, fails[0], "snapshot cm.yaml[0]: diff (-expected +got):")
	assert.Contains(t, fails[0], "-  name: other")
	assert.Contains(t, fails[0], "+  name: cm1")

	dir = writeFiles(t, map[string]string{"cm.yaml": cmDoc})
	fails, _ = Eval(dir, Settings{Mode: ModePartial}, r, false)
	assert.Empty(t, fails)
}

func TestEvalDocCountMismatch(t *testing.T) {
	dir := writeFiles(t, map[string]string{"cm.yaml": cmDoc + "---\n" + cmDoc})
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))
	fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
	containsAll(t, fails, "snapshot cm.yaml: expected 2 object(s), got 1")
}

func TestEvalStates(t *testing.T) {
	r := res(nil,
		node("cm", simulate.StateReady, obj(nil)),
		node("svc", simulate.StatePending))

	t.Run("state mismatch", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"_states.yaml": "cm: NotReady\nsvc: Pending\n"})
		fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
		containsAll(t, fails, `snapshot _states.yaml: node "cm": expected NotReady, got Ready`)
	})

	t.Run("unknown node", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"_states.yaml": "cm: Ready\nsvc: Pending\nghost: Ready\n"})
		fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
		containsAll(t, fails, `snapshot _states.yaml: unknown node "ghost"`)
	})

	t.Run("strict missing entry", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"_states.yaml": "cm: Ready\n"})
		fails, _ := Eval(dir, Settings{Mode: ModeStrict}, r, false)
		containsAll(t, fails, `snapshot _states.yaml: missing entry for node "svc"`)
	})

	t.Run("partial ignores missing entry", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"_states.yaml": "cm: Ready\n"})
		fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
		assert.Empty(t, fails)
	})

	t.Run("bad yaml", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"_states.yaml": "cm: [unclosed\n"})
		fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
		containsAll(t, fails, "snapshot _states.yaml:")
	})
}

func TestEvalStatus(t *testing.T) {
	r := res(map[string]interface{}{"replicas": int64(3)},
		node("cm", simulate.StateReady, obj(nil)))

	t.Run("missing file strict non-empty status", func(t *testing.T) {
		dir := writeFiles(t, nil)
		fails, _ := Eval(dir, Settings{Mode: ModeStrict}, r, false)
		containsAll(t, fails, "snapshot: computed status is not empty but _status.yaml is missing")
	})

	t.Run("missing file partial", func(t *testing.T) {
		dir := writeFiles(t, nil)
		fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
		assert.Empty(t, fails)
	})

	t.Run("match", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"_status.yaml": "replicas: 3\n"})
		fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
		assert.Empty(t, fails)
	})

	t.Run("mismatch", func(t *testing.T) {
		dir := writeFiles(t, map[string]string{"_status.yaml": "replicas: 9\n"})
		fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
		require.Len(t, fails, 1)
		assert.Contains(t, fails[0], "snapshot _status.yaml: diff (-expected +got):")
	})

	t.Run("empty status no file", func(t *testing.T) {
		dir := writeFiles(t, nil)
		empty := res(nil, node("cm", simulate.StateReady, obj(nil)))
		fails, _ := Eval(dir, Settings{Mode: ModeStrict}, empty, false)
		assert.NotContains(t, fails, "snapshot: computed status is not empty but _status.yaml is missing")
	})
}

func TestEvalIgnorePaths(t *testing.T) {
	p, err := fieldpath.Parse("metadata.annotations")
	require.NoError(t, err)
	p2, err := fieldpath.Parse("data.junk[1]")
	require.NoError(t, err)
	s := Settings{Mode: ModePartial, Ignore: []fieldpath.Path{p, p2}}

	r := res(nil, node("cm", simulate.StateReady,
		obj(map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":        "cm1",
				"annotations": map[string]interface{}{"a": "b"},
			},
			"data": map[string]interface{}{"junk": []interface{}{"x", "y", "z"}},
		})))

	dir := writeFiles(t, map[string]string{"cm.yaml": `apiVersion: v1
kind: ConfigMap
metadata: {name: cm1}
data: {junk: [x, z]}
`})
	fails, _ := Eval(dir, s, r, false)
	assert.Empty(t, fails)

	// The result object itself is not mutated.
	ann, found, _ := unstructured.NestedMap(r.Nodes[0].Objects[0].Object, "metadata", "annotations")
	assert.True(t, found)
	assert.Equal(t, map[string]interface{}{"a": "b"}, ann)

	// Ignored status field.
	s2 := Settings{Mode: ModePartial, Ignore: []fieldpath.Path{mustParse(t, "volatile")}}
	r2 := res(map[string]interface{}{"replicas": int64(3), "volatile": "x"},
		node("cm", simulate.StateReady, obj(nil)))
	dir = writeFiles(t, map[string]string{"_status.yaml": "replicas: 3\n"})
	fails, _ = Eval(dir, s2, r2, false)
	assert.Empty(t, fails)
}

func mustParse(t *testing.T, s string) fieldpath.Path {
	t.Helper()
	p, err := fieldpath.Parse(s)
	require.NoError(t, err)
	return p
}

func TestEvalOperatorRejected(t *testing.T) {
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))
	dir := writeFiles(t, map[string]string{"cm.yaml": `apiVersion: v1
kind: ConfigMap
metadata: {name: cm1}
data:
  $regex: ".*"
`})
	fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
	containsAll(t, fails, "snapshot cm.yaml: operator $regex not allowed")

	dir = writeFiles(t, map[string]string{"_status.yaml": "replicas: {$any: true}\n"})
	r2 := res(map[string]interface{}{"replicas": int64(3)})
	fails, _ = Eval(dir, Settings{Mode: ModePartial}, r2, false)
	containsAll(t, fails, "snapshot _status.yaml: operator $any not allowed")
}

func TestEvalUnusableNodeID(t *testing.T) {
	r := res(nil, node("a/b", simulate.StateReady, obj(nil)))
	dir := writeFiles(t, nil)
	for _, update := range []bool{false, true} {
		fails, info := Eval(dir, Settings{Mode: ModePartial}, r, update)
		containsAll(t, fails, `snapshot: node id "a/b" is not usable in a file name`)
		assert.Empty(t, info)
	}
	// A node without objects does not need a file name.
	r = res(nil, node("a/b", simulate.StateIgnored))
	fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
	assert.Empty(t, fails)
}

func TestEvalUnreadableFile(t *testing.T) {
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))
	dir := writeFiles(t, map[string]string{"cm.yaml": "apiVersion: [unclosed\n"})
	fails, _ := Eval(dir, Settings{Mode: ModePartial}, r, false)
	containsAll(t, fails, "snapshot cm.yaml:")
}

func TestEvalUpdate(t *testing.T) {
	r := res(map[string]interface{}{"replicas": int64(3)},
		node("cm", simulate.StateReady, obj(nil)),
		node("svc", simulate.StatePending))
	dir := filepath.Join(t.TempDir(), "expected")

	fails, info := Eval(dir, Settings{Mode: ModeStrict}, r, true)
	assert.Empty(t, fails)
	require.Equal(t, []string{"snapshot: wrote " + dir + " (3 files)"}, info)

	var names []string
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	assert.Equal(t, []string{"_states.yaml", "_status.yaml", "cm.yaml"}, names)

	states, err := os.ReadFile(filepath.Join(dir, "_states.yaml")) // #nosec G304 -- test
	require.NoError(t, err)
	assert.Equal(t, "cm: Ready\nsvc: Pending\n", string(states))
	status, err := os.ReadFile(filepath.Join(dir, "_status.yaml")) // #nosec G304 -- test
	require.NoError(t, err)
	assert.Equal(t, "replicas: 3\n", string(status))

	// What was written compares clean.
	fails, _ = Eval(dir, Settings{Mode: ModeStrict}, r, false)
	assert.Empty(t, fails)

	// Update again is byte-identical.
	snapshot := readDirBytes(t, dir)
	_, info = Eval(dir, Settings{Mode: ModeStrict}, r, true)
	require.NotEmpty(t, info)
	assert.Equal(t, snapshot, readDirBytes(t, dir))
}

func TestEvalUpdatePrunesStale(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"stale.yaml": cmDoc,
		"old.yml":    cmDoc,
		"keep.txt":   "not yaml",
	})
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "x.yaml"), []byte("a: 1\n"), 0o600))

	r := res(nil, node("cm", simulate.StateReady, obj(nil)))
	fails, info := Eval(dir, Settings{Mode: ModeStrict}, r, true)
	assert.Empty(t, fails)
	require.Len(t, info, 1)

	var names []string
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	assert.Equal(t, []string{"_states.yaml", "cm.yaml", "keep.txt", "sub"}, names)
	// No _status.yaml: the instance status is empty.
}

func TestEvalUpdateOff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "expected")
	r := res(nil, node("cm", simulate.StateReady, obj(nil)))
	fails, info := Eval(dir, Settings{Mode: ModeOff}, r, true)
	assert.Empty(t, fails)
	assert.Empty(t, info)
	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err))
}

func readDirBytes(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- test
		require.NoError(t, err)
		out[e.Name()] = data
	}
	return out
}
