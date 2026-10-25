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

package run

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/ktest/discover"
	"github.com/Matthew-Beckett/krobar/internal/ktest/spec"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
	"github.com/Matthew-Beckett/krobar/internal/simulate"
)

const miniRGD = `
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: minis.kro.run
spec:
  schema:
    apiVersion: v1alpha1
    kind: Mini
    spec:
      name: string
      replicas: integer | default=1
  resources:
    - id: cm
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${schema.spec.name}-cm
        data:
          note: ${schema.spec.name}
`

func miniInstance(replicas interface{}) map[string]interface{} {
	spec := map[string]interface{}{"name": "m"}
	if replicas != nil {
		if i, ok := replicas.(int); ok {
			replicas = int64(i)
		}
		spec["replicas"] = replicas
	}
	return map[string]interface{}{
		"apiVersion": "kro.run/v1alpha1",
		"kind":       "Mini",
		"metadata":   map[string]interface{}{"name": "m", "namespace": "default"},
		"spec":       spec,
	}
}

type collector struct{ events []Event }

func (c *collector) Event(ev Event) { c.events = append(c.events, ev) }

func (c *collector) terminal() []Event {
	var out []Event
	for _, e := range c.events {
		if e.Action != ActionRun {
			out = append(out, e)
		}
	}
	return out
}

func mkCase(name string, inst map[string]interface{}, e *spec.Expect) *discover.Case {
	return &discover.Case{
		Name: name,
		Steps: []*discover.Step{{
			Instance: &unstructured.Unstructured{Object: inst},
			Expect:   e,
		}},
	}
}

func miniPlan(t *testing.T, cases ...*discover.Case) *discover.Plan {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rgd.yaml"), []byte(miniRGD), 0o600))
	return &discover.Plan{Suites: []*discover.Suite{{
		Name:  "mini",
		Root:  dir,
		RGD:   filepath.Join(dir, "rgd.yaml"),
		Cases: cases,
	}}}
}

func runPlan(p *discover.Plan, env *clienv.Env) (*collector, Summary) {
	c := &collector{}
	if env == nil {
		env = clienv.Default()
	}
	r := &Runner{Env: env, Sinks: []Sink{c}}
	return c, r.Run(p)
}

func TestPassAndMismatchMessages(t *testing.T) {
	pass := mkCase("ok", miniInstance(3), &spec.Expect{
		Nodes: map[string]spec.NodeExpect{
			"cm": {State: "Ready", Count: intPtr(1)},
		},
	})
	fail := mkCase("bad", miniInstance(nil), &spec.Expect{
		Nodes: map[string]spec.NodeExpect{
			"cm":    {Objects: []map[string]interface{}{{"metadata": map[string]interface{}{"name": "other-cm"}}}},
			"ghost": {State: "Ready"},
		},
	})
	c, sum := runPlan(miniPlan(t, pass, fail), nil)
	assert.Equal(t, Summary{Passed: 1, Failed: 1}, sum)
	term := c.terminal()
	require.Len(t, term, 2)
	assert.Equal(t, ActionPass, term[0].Action)
	assert.Equal(t, "mini/ok", term[0].Path)
	assert.Equal(t, ActionFail, term[1].Action)
	assert.Contains(t, term[1].Messages, "nodes.cm.objects[0].metadata.name: expected other-cm, got m-cm")
	assert.Contains(t, term[1].Messages, "nodes.ghost: no such node in result")
}

func TestExpectedError(t *testing.T) {
	bad := miniInstance("nope")
	contains := mkCase("contains", bad, &spec.Expect{
		Error: &spec.ErrorExpect{Contains: "instance is invalid"},
	})
	regex := mkCase("regex", bad, &spec.Expect{
		Error: &spec.ErrorExpect{Regex: "instance is inv.*"},
	})
	miss := mkCase("miss", bad, &spec.Expect{
		Error: &spec.ErrorExpect{Contains: "something else"},
	})
	succeeds := mkCase("succeeds", miniInstance(nil), &spec.Expect{
		Error: &spec.ErrorExpect{Contains: "instance is invalid"},
	})
	c, sum := runPlan(miniPlan(t, contains, regex, miss, succeeds), nil)
	assert.Equal(t, Summary{Passed: 2, Failed: 2}, sum)
	term := c.terminal()
	require.Len(t, term, 4)
	assert.Equal(t, ActionPass, term[0].Action)
	assert.Equal(t, ActionPass, term[1].Action)
	require.Len(t, term[2].Messages, 1)
	assert.Contains(t, term[2].Messages[0], `expected error containing "something else", got "instance is invalid`)
	require.Len(t, term[3].Messages, 1)
	assert.Contains(t, term[3].Messages[0], `expected error containing "instance is invalid", but the run succeeded`)
}

func TestGraphBuildErrorSharedAndMatchable(t *testing.T) {
	dir := t.TempDir()
	p := &discover.Plan{Suites: []*discover.Suite{{
		Name: "brokensuite",
		Root: dir,
		RGD:  filepath.Join(dir, "missing.yaml"),
		Cases: []*discover.Case{
			mkCase("expects-error", miniInstance(nil), &spec.Expect{
				Error: &spec.ErrorExpect{Contains: "loading RGD"},
			}),
			mkCase("expects-ready", miniInstance(nil), &spec.Expect{
				Nodes: map[string]spec.NodeExpect{"cm": {State: "Ready"}},
			}),
		},
	}}}
	c, sum := runPlan(p, nil)
	assert.Equal(t, Summary{Passed: 1, Failed: 1}, sum)
	term := c.terminal()
	require.Len(t, term, 2)
	assert.Equal(t, ActionPass, term[0].Action)
	require.Len(t, term[1].Messages, 1)
	assert.Contains(t, term[1].Messages[0], "run failed: loading RGD")
}

func TestGraphBuiltOncePerSuite(t *testing.T) {
	env := clienv.Default()
	builds := 0
	inner := env.NewOfflineGraphBuilder
	env.NewOfflineGraphBuilder = func(paths []string) (rgd.GraphBuilder, error) {
		builds++
		return inner(paths)
	}
	a := mkCase("a", miniInstance(nil), nil)
	b := mkCase("b", miniInstance(nil), nil)
	_, sum := runPlan(miniPlan(t, a, b), env)
	assert.Equal(t, Summary{Passed: 2}, sum)
	assert.Equal(t, 1, builds)
}

func TestUnmatchedObserved(t *testing.T) {
	extra := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": "extra", "namespace": "default"},
	}}
	expectNone := mkCase("expects-none", miniInstance(nil), &spec.Expect{
		UnmatchedObserved: intPtr(0),
	})
	expectNone.Steps[0].Observed = []*unstructured.Unstructured{extra}
	expectOne := mkCase("expects-one", miniInstance(nil), &spec.Expect{
		UnmatchedObserved: intPtr(1),
	})
	expectOne.Steps[0].Observed = []*unstructured.Unstructured{extra}
	c, sum := runPlan(miniPlan(t, expectNone, expectOne), nil)
	assert.Equal(t, Summary{Passed: 1, Failed: 1}, sum)
	term := c.terminal()
	require.Len(t, term, 2)
	require.Len(t, term[0].Messages, 1)
	assert.Contains(t, term[0].Messages[0], "unmatchedObserved: expected 0, got 1 (/v1, Kind=ConfigMap default/extra)")
	assert.Equal(t, ActionPass, term[1].Action)
}

func TestSkipFixtureErrorAndSuiteError(t *testing.T) {
	skipped := &discover.Case{Name: "lazy", Skip: "not ready"}
	broken := &discover.Case{Name: "broken", Err: fmt.Errorf("bad case.yaml")}
	p := miniPlan(t, skipped, broken)
	p.Suites = append(p.Suites, &discover.Suite{
		Name: "badsuite",
		Err:  fmt.Errorf("invalid manifest"),
	})
	c, sum := runPlan(p, nil)
	assert.Equal(t, Summary{Failed: 2, Skipped: 1}, sum)
	term := c.terminal()
	require.Len(t, term, 3)
	assert.Equal(t, ActionSkip, term[0].Action)
	assert.Equal(t, []string{"not ready"}, term[0].Messages)
	assert.Equal(t, ActionFail, term[1].Action)
	assert.Equal(t, []string{"fixture error: bad case.yaml"}, term[1].Messages)
	assert.Equal(t, ActionFail, term[2].Action)
	assert.Equal(t, "badsuite", term[2].Path)
	assert.Empty(t, term[2].Case)
	assert.Equal(t, []string{"suite error: invalid manifest"}, term[2].Messages)
}

func TestFilter(t *testing.T) {
	a := mkCase("a", miniInstance(nil), nil)
	b := mkCase("b", miniInstance(nil), nil)
	c := &collector{}
	r := &Runner{
		Env:   clienv.Default(),
		Sinks: []Sink{c},
		Filter: func(path string) bool {
			return path == "mini/a"
		},
	}
	sum := r.Run(miniPlan(t, a, b))
	assert.Equal(t, Summary{Passed: 1}, sum)
	require.Len(t, c.terminal(), 1)
	assert.Equal(t, "mini/a", c.terminal()[0].Path)
}

func TestClusterBuilderNotUsed(t *testing.T) {
	// The default offline path never resolves a rest.Config.
	env := clienv.Default()
	env.ResolveRESTConfig = func(_, _ string) (*rest.Config, error) {
		return nil, fmt.Errorf("must not be called")
	}
	a := mkCase("a", miniInstance(nil), nil)
	_, sum := runPlan(miniPlan(t, a), env)
	assert.Equal(t, Summary{Passed: 1}, sum)
}

func TestSnapshotMismatchFails(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata: {name: other-cm}
data: {note: m}
`), 0o600))
	c := mkCase("snap", miniInstance(nil), nil)
	c.SnapshotDir = dir
	p := miniPlan(t, c)
	p.Suites[0].SnapshotMode = "strict"
	col, sum := runPlan(p, nil)
	assert.Equal(t, Summary{Failed: 1}, sum)
	term := col.terminal()
	require.Len(t, term, 1)
	var joined string
	for _, m := range term[0].Messages {
		joined += m + "\n"
	}
	assert.Contains(t, joined, "snapshot cm.yaml[0]: diff (-expected +got):")
	assert.Contains(t, joined, "-  name: other-cm")
	assert.Contains(t, joined, "+  name: m-cm")
	assert.Contains(t, joined, `snapshot _states.yaml: missing entry for node "cm"`)
}

func TestSnapshotUpdateWritesAndPasses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "expected")
	c := mkCase("snap", miniInstance(nil), nil)
	c.SnapshotDir = dir
	p := miniPlan(t, c)
	p.Suites[0].SnapshotMode = "strict"

	col := &collector{}
	r := &Runner{Env: clienv.Default(), Sinks: []Sink{col}, Update: true}
	sum := r.Run(p)
	assert.Equal(t, Summary{Passed: 1}, sum)
	term := col.terminal()
	require.Len(t, term, 1)
	assert.Equal(t, ActionPass, term[0].Action)
	require.Len(t, term[0].Info, 1)
	assert.Contains(t, term[0].Info[0], "snapshot: wrote "+dir)
	assert.FileExists(t, filepath.Join(dir, "cm.yaml"))
	assert.FileExists(t, filepath.Join(dir, "_states.yaml"))

	// The written snapshots compare clean on a normal run.
	col2, sum2 := runPlan(p, nil)
	assert.Equal(t, Summary{Passed: 1}, sum2)
	assert.Empty(t, col2.terminal()[0].Messages)
}

func TestSnapshotUpdateSkipsUntouched(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "expected")
	skipped := &discover.Case{Name: "lazy", Skip: "not ready", SnapshotDir: dir}
	broken := &discover.Case{Name: "broken", Err: fmt.Errorf("bad case.yaml"), SnapshotDir: dir}
	filtered := mkCase("filtered", miniInstance(nil), nil)
	filtered.SnapshotDir = dir
	errExpect := mkCase("err", miniInstance("nope"), &spec.Expect{
		Error: &spec.ErrorExpect{Contains: "instance is invalid"},
	})
	errExpect.SnapshotDir = dir
	p := miniPlan(t, skipped, broken, filtered, errExpect)
	p.Suites[0].SnapshotMode = "strict"

	col := &collector{}
	r := &Runner{
		Env:    clienv.Default(),
		Sinks:  []Sink{col},
		Update: true,
		Filter: func(path string) bool { return path != "mini/filtered" },
	}
	sum := r.Run(p)
	assert.Equal(t, Summary{Passed: 1, Failed: 1, Skipped: 1}, sum)
	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "no snapshot files may be written")
}

func intPtr(i int) *int { return &i }

func mkAssertCase(name string, inst map[string]interface{}, asserts ...spec.Assertion) *discover.Case {
	c := mkCase(name, inst, nil)
	c.Steps[0].Assert = asserts
	return c
}

// fakeResult builds a simulation result with the given nodes for direct
// assertion evaluation tests.
func fakeResult(objs map[string][]*unstructured.Unstructured, states map[string]simulate.State) *simulate.Result {
	res := &simulate.Result{
		Instance: &unstructured.Unstructured{Object: miniInstance(nil)},
	}
	for id, os := range objs {
		res.Nodes = append(res.Nodes, simulate.NodeResult{
			ID: id, State: states[id], Objects: os,
		})
	}
	return res
}

func obj(name string, kv map[string]interface{}) *unstructured.Unstructured {
	o := map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": name, "namespace": "default"},
	}
	for k, v := range kv {
		o[k] = v
	}
	return &unstructured.Unstructured{Object: o}
}

func TestAssertionSubjects(t *testing.T) {
	res := fakeResult(
		map[string][]*unstructured.Unstructured{
			"cm":    {obj("cm1", map[string]interface{}{"data": map[string]interface{}{"x": "1"}})},
			"multi": {obj("m1", nil), obj("m2", nil)},
		},
		map[string]simulate.State{"cm": simulate.StateReady, "multi": simulate.StateReady},
	)

	t.Run("instance subject", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Match: map[string]interface{}{"spec": map[string]interface{}{"name": "m"}},
		}}}
		assert.Empty(t, checkAssertions(step, res))
	})

	t.Run("single object default", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "cm", Match: map[string]interface{}{"metadata": map[string]interface{}{"name": "cm1"}},
		}}}
		assert.Empty(t, checkAssertions(step, res))
	})

	t.Run("index", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "multi", Index: intPtr(1),
			Match: map[string]interface{}{"metadata": map[string]interface{}{"name": "m2"}},
		}}}
		assert.Empty(t, checkAssertions(step, res))
	})

	t.Run("index out of range", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "multi", Index: intPtr(9),
			Match: map[string]interface{}{"a": 1},
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: node "multi" has 2 object(s), index 9 out of range`}, msgs)
	})

	t.Run("select ok", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "multi", Select: &spec.Selector{Name: "m2"},
			Match: map[string]interface{}{"metadata": map[string]interface{}{"name": "m2"}},
		}}}
		assert.Empty(t, checkAssertions(step, res))
	})

	t.Run("select none", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "multi", Select: &spec.Selector{Kind: "Secret"},
			Match: map[string]interface{}{"a": 1},
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: node "multi": select matched 0 object(s), want exactly 1`}, msgs)
	})

	t.Run("select ambiguous", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "multi", Select: &spec.Selector{Kind: "ConfigMap"},
			Match: map[string]interface{}{"a": 1},
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: node "multi": select matched 2 object(s), want exactly 1`}, msgs)
	})

	t.Run("each with one failing object", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "multi", Each: true,
			Match: map[string]interface{}{"metadata": map[string]interface{}{"name": "m1"}},
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: nodes.multi.objects[1].metadata.name: expected m1, got m2`}, msgs)
	})

	t.Run("each on empty node", func(t *testing.T) {
		empty := fakeResult(
			map[string][]*unstructured.Unstructured{"cm": {}},
			map[string]simulate.State{"cm": simulate.StatePending},
		)
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "cm", Each: true,
			Match: map[string]interface{}{"a": 1},
		}}}
		msgs := checkAssertions(step, empty)
		assert.Equal(t, []string{`assert[0]: node "cm" has 0 object(s); each requires at least one`}, msgs)
	})

	t.Run("no selector requires one object", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "multi", Match: map[string]interface{}{"a": 1},
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: node "multi" has 2 object(s); use index, select or each`}, msgs)
	})

	t.Run("path not found", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "cm", Path: "spec.missing",
			Match: map[string]interface{}{"a": 1},
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: nodes.cm.objects[0].spec.missing: not found`}, msgs)
	})

	t.Run("missing node", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "ghost", Match: map[string]interface{}{"a": 1},
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: no such node "ghost"`}, msgs)
	})

	t.Run("state and count", func(t *testing.T) {
		step := &discover.Step{Assert: []spec.Assertion{{
			Node: "cm", State: "Ready", Count: intPtr(2),
		}}}
		msgs := checkAssertions(step, res)
		assert.Equal(t, []string{`assert[0]: count: expected 2, got 1`}, msgs)
	})
}

func TestAssertionRuntime(t *testing.T) {
	t.Run("equals failure with diff", func(t *testing.T) {
		c := mkAssertCase("eq", miniInstance(nil), spec.Assertion{
			Name: "spec-eq",
			Equals: map[string]interface{}{
				"spec": map[string]interface{}{"name": "m", "replicas": 9},
			},
		})
		col, sum := runPlan(miniPlan(t, c), nil)
		assert.Equal(t, Summary{Failed: 1}, sum)
		term := col.terminal()
		require.Len(t, term, 1)
		var joined string
		for _, m := range term[0].Messages {
			joined += m + "\n"
		}
		assert.Contains(t, joined, `assert "spec-eq": instance.spec.replicas: expected 9, got 1`)
		assert.Contains(t, joined, "diff (-expected +got):")
		assert.Contains(t, joined, "-  replicas: 9")
		assert.Contains(t, joined, "+  replicas: 1")
	})

	t.Run("absent", func(t *testing.T) {
		ok := mkAssertCase("ok", miniInstance(nil), spec.Assertion{
			Absent: []string{"spec.missing", "metadata.labels"},
		})
		bad := mkAssertCase("bad", miniInstance(nil), spec.Assertion{
			Absent: []string{"spec.name", "status"},
		})
		col, sum := runPlan(miniPlan(t, ok, bad), nil)
		assert.Equal(t, Summary{Passed: 1, Failed: 1}, sum)
		term := col.terminal()
		assert.Equal(t, []string{
			`assert[0]: instance.spec.name: expected absent, got m`,
			`assert[0]: instance.status: expected absent, got map[]`,
		}, term[1].Messages)
	})

	t.Run("cel", func(t *testing.T) {
		tru := mkAssertCase("tru", miniInstance(nil), spec.Assertion{
			Node: "cm", CEL: `self.metadata.name == instance.spec.name + "-cm"`,
		})
		fal := mkAssertCase("fal", miniInstance(nil), spec.Assertion{
			Node: "cm", CEL: `self.metadata.name == "nope"`,
		})
		errc := mkAssertCase("err", miniInstance(nil), spec.Assertion{
			Node: "cm", CEL: `self.data.missing[3] > 0`,
		})
		xnum := mkAssertCase("xnum", miniInstance(3), spec.Assertion{
			Node: "cm", CEL: `nodes.cm.objects[0].data.note == instance.spec.name`,
		})
		col, sum := runPlan(miniPlan(t, tru, fal, errc, xnum), nil)
		assert.Equal(t, Summary{Passed: 2, Failed: 2}, sum)
		term := col.terminal()
		assert.Equal(t, ActionPass, term[0].Action)
		assert.Contains(t, term[1].Messages[0], `cel "self.metadata.name == \"nope\"" was false`)
		assert.Contains(t, term[2].Messages[0], `assert[0]: cel "self.data.missing[3] > 0":`)
		assert.Equal(t, ActionPass, term[3].Action)
	})

	t.Run("cross type numeric", func(t *testing.T) {
		c := mkAssertCase("x", miniInstance(3), spec.Assertion{
			Node: "cm",
			// self.spec replicas is absent on a ConfigMap; compare against
			// the instance's float64-decoded replicas via a literal.
			CEL: `instance.spec.replicas == 3`,
		})
		_, sum := runPlan(miniPlan(t, c), nil)
		assert.Equal(t, Summary{Passed: 1}, sum)
	})

	t.Run("labels", func(t *testing.T) {
		named := mkAssertCase("named", miniInstance(nil), spec.Assertion{
			Name: "my-check", Node: "ghost", Match: map[string]interface{}{"a": 1},
		})
		unnamed := mkAssertCase("unnamed", miniInstance(nil), spec.Assertion{
			Node: "ghost", Match: map[string]interface{}{"a": 1},
		})
		col, sum := runPlan(miniPlan(t, named, unnamed), nil)
		assert.Equal(t, Summary{Failed: 2}, sum)
		term := col.terminal()
		assert.Contains(t, term[0].Messages[0], `assert "my-check":`)
		assert.Contains(t, term[1].Messages[0], `assert[0]:`)
	})

	t.Run("expect and assert combined", func(t *testing.T) {
		c := mkCase("both", miniInstance(nil), &spec.Expect{
			Nodes: map[string]spec.NodeExpect{"ghost": {State: "Ready"}},
		})
		c.Steps[0].Assert = []spec.Assertion{{
			Node: "cm", Count: intPtr(5),
		}}
		col, sum := runPlan(miniPlan(t, c), nil)
		assert.Equal(t, Summary{Failed: 1}, sum)
		term := col.terminal()
		assert.Contains(t, term[0].Messages, "nodes.ghost: no such node in result")
		assert.Contains(t, term[0].Messages, `assert[0]: count: expected 5, got 1`)
	})
}
