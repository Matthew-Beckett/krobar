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

// Package snapshot compares a simulation result against per-case snapshot
// files, or rewrites them in update mode. A snapshot directory holds one
// multi-document <nodeID>.yaml per rendered node, a _states.yaml mapping
// node IDs to their state, and an optional _status.yaml with the
// instance's computed status.
package snapshot

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	yaml "go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/Matthew-Beckett/krobar/internal/ktest/fieldpath"
	"github.com/Matthew-Beckett/krobar/internal/ktest/match"
	"github.com/Matthew-Beckett/krobar/internal/objects"
	"github.com/Matthew-Beckett/krobar/internal/simulate"
)

// Snapshot modes.
const (
	ModeStrict  = "strict"
	ModePartial = "partial"
	ModeOff     = "off"
)

const (
	statesFile = "_states.yaml"
	statusFile = "_status.yaml"
)

// Settings configures Eval.
type Settings struct {
	// Mode is strict, partial or off; empty behaves as strict.
	Mode string
	// Ignore lists field paths stripped from rendered objects and the
	// instance status before comparison and before writing.
	Ignore []fieldpath.Path
}

// Eval compares res against the snapshot files under dir, or rewrites
// them when update is true. It returns failure messages and, in update
// mode, informational messages describing what was written.
func Eval(dir string, s Settings, res *simulate.Result, update bool) (failures, info []string) {
	if dir == "" || s.Mode == ModeOff {
		return nil, nil
	}
	if update {
		return updateDir(dir, s, res)
	}
	return compareDir(dir, s.Mode, s, res)
}

var usableNodeID = regexp.MustCompile(`^[A-Za-z0-9-._]+$`)

// usableName reports whether id works as a snapshot file name.
func usableName(id string) bool {
	return usableNodeID.MatchString(id) && !strings.HasPrefix(id, "_")
}

// unusableIDs returns one failure message per node that rendered objects
// but cannot map to a snapshot file name.
func unusableIDs(res *simulate.Result) []string {
	var fails []string
	for _, n := range res.Nodes {
		if len(n.Objects) > 0 && !usableName(n.ID) {
			fails = append(fails, fmt.Sprintf("snapshot: node id %q is not usable in a file name", n.ID))
		}
	}
	return fails
}

// compareDir checks the result against the snapshot files under dir.
func compareDir(dir, mode string, s Settings, res *simulate.Result) ([]string, []string) {
	fails := unusableIDs(res)
	entries, err := os.ReadDir(dir)
	if err != nil {
		switch {
		case os.IsNotExist(err) && mode == ModePartial:
		case os.IsNotExist(err):
			fails = append(fails, fmt.Sprintf("snapshot: snapshot directory %s does not exist", dir))
		default:
			fails = append(fails, fmt.Sprintf("snapshot: reading %s: %s", dir, err))
		}
		return fails, nil
	}
	nodeFiles, statesPath, statusPath := listFiles(dir, entries)

	byID := make(map[string]simulate.NodeResult, len(res.Nodes))
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	for _, n := range res.Nodes {
		f, ok := nodeFiles[n.ID]
		if !ok {
			if mode == ModeStrict && len(n.Objects) > 0 && usableName(n.ID) {
				fails = append(fails, fmt.Sprintf("snapshot: node %q rendered %d object(s) but has no snapshot file", n.ID, len(n.Objects)))
			}
			continue
		}
		delete(nodeFiles, n.ID)
		docs, err := readNodeFile(f)
		if err != nil {
			fails = append(fails, fmt.Sprintf("snapshot %s: %s", filepath.Base(f), err))
			continue
		}
		fails = append(fails, compareNode(f, n, docs, s)...)
	}
	for _, id := range sortedKeys(nodeFiles) {
		f := nodeFiles[id]
		docs, err := readNodeFile(f)
		if err != nil {
			fails = append(fails, fmt.Sprintf("snapshot %s: %s", filepath.Base(f), err))
			continue
		}
		if len(docs) > 0 {
			fails = append(fails, fmt.Sprintf("snapshot: node %q has a snapshot file but the result has no such node", id))
		}
	}
	fails = append(fails, compareStates(statesPath, mode, res, byID)...)
	fails = append(fails, compareStatus(statusPath, mode, s, res)...)
	return fails, nil
}

// listFiles indexes the yaml files under dir: node files by node ID, plus
// the _states/_status paths.
func listFiles(dir string, entries []os.DirEntry) (nodes map[string]string, states, status string) {
	nodes = map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ext)
		switch name {
		case "_states":
			states = filepath.Join(dir, e.Name())
		case "_status":
			status = filepath.Join(dir, e.Name())
		default:
			nodes[name] = filepath.Join(dir, e.Name())
		}
	}
	return nodes, states, status
}

// readNodeFile parses a node snapshot file into its object documents.
func readNodeFile(path string) ([]map[string]interface{}, error) {
	objs, err := objects.ReadFile(path, nil)
	if err != nil {
		return nil, err
	}
	docs := make([]map[string]interface{}, 0, len(objs))
	for _, o := range objs {
		docs = append(docs, o.Object)
	}
	return docs, nil
}

// compareNode checks one node's rendered objects against its snapshot
// file's documents.
func compareNode(file string, n simulate.NodeResult, docs []map[string]interface{}, s Settings) []string {
	base := filepath.Base(file)
	switch {
	case len(docs) > 0 && len(n.Objects) == 0:
		return []string{fmt.Sprintf("snapshot: node %q has a snapshot file but rendered no objects", n.ID)}
	case len(docs) != len(n.Objects):
		return []string{fmt.Sprintf("snapshot %s: expected %d object(s), got %d", base, len(docs), len(n.Objects))}
	}
	var fails []string
	for i, want := range docs {
		if op := findOperator(want); op != "" {
			fails = append(fails, fmt.Sprintf("snapshot %s: operator %s not allowed", base, op))
			continue
		}
		got := stripped(n.Objects[i].Object, s.Ignore)
		if len(match.Exact(want, got, "")) > 0 {
			fails = append(fails, fmt.Sprintf("snapshot %s[%d]: diff (-expected +got):\n%s", base, i, yamlDiff(want, got)))
		}
	}
	return fails
}

// compareStates checks the _states.yaml file: every entry must name a
// result node with the matching state, and strict mode additionally
// requires an entry per result node.
func compareStates(path, mode string, res *simulate.Result, byID map[string]simulate.NodeResult) []string {
	want := map[string]string{}
	if path != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- suite fixture path
		if err != nil {
			return []string{fmt.Sprintf("snapshot %s: %s", statesFile, err)}
		}
		if err := sigsyaml.Unmarshal(data, &want); err != nil {
			return []string{fmt.Sprintf("snapshot %s: %s", statesFile, err)}
		}
	} else if mode != ModeStrict {
		return nil
	}
	var fails []string
	for _, id := range sortedKeys(want) {
		n, ok := byID[id]
		if !ok {
			fails = append(fails, fmt.Sprintf("snapshot %s: unknown node %q", statesFile, id))
			continue
		}
		if string(n.State) != want[id] {
			fails = append(fails, fmt.Sprintf("snapshot %s: node %q: expected %s, got %s", statesFile, id, want[id], n.State))
		}
	}
	if mode == ModeStrict {
		for _, n := range res.Nodes {
			if _, ok := want[n.ID]; !ok {
				fails = append(fails, fmt.Sprintf("snapshot %s: missing entry for node %q", statesFile, n.ID))
			}
		}
	}
	return fails
}

// compareStatus checks the _status.yaml file against the instance's
// computed status (post-ignore).
func compareStatus(path, mode string, s Settings, res *simulate.Result) []string {
	got := statusOf(res, s)
	if got == nil {
		got = map[string]interface{}{}
	}
	if path == "" {
		if mode == ModeStrict && len(got) > 0 {
			return []string{"snapshot: computed status is not empty but _status.yaml is missing"}
		}
		return nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- suite fixture path
	if err != nil {
		return []string{fmt.Sprintf("snapshot %s: %s", statusFile, err)}
	}
	var want map[string]interface{}
	if err := sigsyaml.Unmarshal(data, &want); err != nil {
		return []string{fmt.Sprintf("snapshot %s: %s", statusFile, err)}
	}
	if want == nil {
		want = map[string]interface{}{}
	}
	if op := findOperator(want); op != "" {
		return []string{fmt.Sprintf("snapshot %s: operator %s not allowed", statusFile, op)}
	}
	if len(match.Exact(want, got, "")) > 0 {
		return []string{fmt.Sprintf("snapshot %s: diff (-expected +got):\n%s", statusFile, yamlDiff(want, got))}
	}
	return nil
}

// statusOf returns the instance's computed status with the ignore paths
// stripped, or nil when absent.
func statusOf(res *simulate.Result, s Settings) map[string]interface{} {
	m, found, _ := unstructured.NestedMap(res.Instance.Object, "status")
	if !found {
		return nil
	}
	return stripped(m, s.Ignore)
}

// updateDir rewrites the snapshot directory from the result: one
// <nodeID>.yaml per node with rendered objects, _states.yaml always,
// _status.yaml when the status is non-empty; any other yaml file is
// pruned.
func updateDir(dir string, s Settings, res *simulate.Result) ([]string, []string) {
	if fails := unusableIDs(res); len(fails) > 0 {
		return fails, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return []string{fmt.Sprintf("snapshot: %s", err)}, nil
	}
	var fails []string
	written := map[string]bool{}
	files := 0
	write := func(name string, docs ...interface{}) {
		data, err := encode(docs...)
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), data, 0o600)
		}
		if err != nil {
			fails = append(fails, fmt.Sprintf("snapshot: writing %s: %s", name, err))
			return
		}
		written[name] = true
		files++
	}
	for _, n := range res.Nodes {
		if len(n.Objects) == 0 {
			continue
		}
		docs := make([]interface{}, len(n.Objects))
		for i, o := range n.Objects {
			docs[i] = stripped(o.Object, s.Ignore)
		}
		write(n.ID+".yaml", docs...)
	}
	states := make(map[string]string, len(res.Nodes))
	for _, n := range res.Nodes {
		states[n.ID] = string(n.State)
	}
	write(statesFile, states)
	if status := statusOf(res, s); len(status) > 0 {
		write(statusFile, status)
	}
	fails = append(fails, pruneStale(dir, written)...)
	if len(fails) > 0 {
		return fails, nil
	}
	return nil, []string{fmt.Sprintf("snapshot: wrote %s (%d files)", dir, files)}
}

// pruneStale removes every *.yaml/*.yml file under dir not in written.
func pruneStale(dir string, written map[string]bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{fmt.Sprintf("snapshot: reading %s: %s", dir, err)}
	}
	var fails []string
	for _, e := range entries {
		if e.IsDir() || written[e.Name()] {
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".yaml", ".yml":
		default:
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			fails = append(fails, fmt.Sprintf("snapshot: removing %s: %s", e.Name(), err))
		}
	}
	return fails
}

// encode serialises each value as one YAML document in a single stream.
func encode(docs ...interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// stripped returns m with every ignore path deleted. The input map is
// not modified.
func stripped(m map[string]interface{}, ignore []fieldpath.Path) map[string]interface{} {
	if len(ignore) == 0 {
		return m
	}
	out := deepCopy(m).(map[string]interface{})
	for _, p := range ignore {
		out = deleteValue(out, p).(map[string]interface{})
	}
	return out
}

// deleteValue removes the value at p within v and returns the updated v.
// Missing keys, out-of-range indexes and wrong-type steps are no-ops.
func deleteValue(v interface{}, p fieldpath.Path) interface{} {
	if len(p) == 0 {
		return v
	}
	seg := p[0]
	if seg.IsIndex {
		list, ok := v.([]interface{})
		if !ok || seg.Index >= len(list) {
			return v
		}
		if len(p) == 1 {
			return append(list[:seg.Index], list[seg.Index+1:]...)
		}
		list[seg.Index] = deleteValue(list[seg.Index], p[1:])
		return list
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	if len(p) == 1 {
		delete(m, seg.Key)
		return m
	}
	if child, ok := m[seg.Key]; ok {
		m[seg.Key] = deleteValue(child, p[1:])
	}
	return m
}

func deepCopy(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, x := range t {
			out[k] = deepCopy(x)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, x := range t {
			out[i] = deepCopy(x)
		}
		return out
	default:
		return v
	}
}

// findOperator returns the first operator key (a $-prefixed key that is
// not a $$ escape) anywhere in v, or "".
func findOperator(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		if list, ok := v.([]interface{}); ok {
			for _, e := range list {
				if op := findOperator(e); op != "" {
					return op
				}
			}
		}
		return ""
	}
	for _, k := range sortedKeys(m) {
		if strings.HasPrefix(k, "$") && !strings.HasPrefix(k, "$$") {
			return k
		}
		if op := findOperator(m[k]); op != "" {
			return op
		}
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func yamlDiff(expected, got interface{}) string {
	ey, err := sigsyaml.Marshal(expected)
	if err != nil {
		ey = fmt.Appendf(nil, "%v\n", expected)
	}
	gy, err := sigsyaml.Marshal(got)
	if err != nil {
		gy = fmt.Appendf(nil, "%v\n", got)
	}
	s, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(ey)),
		B:        difflib.SplitLines(string(gy)),
		FromFile: "expected",
		ToFile:   "got",
		Context:  3,
	})
	if err != nil {
		return ""
	}
	return s
}
