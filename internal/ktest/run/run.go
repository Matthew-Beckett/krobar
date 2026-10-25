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

// Package run executes a discovered test Plan against the simulated
// reconciliation engine and reports per-case events to Sinks.
package run

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	krograph "github.com/kubernetes-sigs/kro/pkg/graph"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/ktest/discover"
	"github.com/Matthew-Beckett/krobar/internal/ktest/match"
	"github.com/Matthew-Beckett/krobar/internal/ktest/snapshot"
	"github.com/Matthew-Beckett/krobar/internal/ktest/spec"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
	"github.com/Matthew-Beckett/krobar/internal/simulate"
)

// Action is the kind of an Event.
type Action string

// Event actions.
const (
	ActionRun  Action = "run"
	ActionPass Action = "pass"
	ActionFail Action = "fail"
	ActionSkip Action = "skip"
)

// Event reports one case starting (ActionRun) or terminating.
type Event struct {
	Suite    string
	Case     string
	Step     string
	Path     string
	File     string
	Action   Action
	Elapsed  time.Duration
	Messages []string
	// Info holds non-failure informational lines (snapshot writes).
	Info []string
}

// Sink receives run events.
type Sink interface {
	// Event is called once per run event, in emission order.
	Event(Event)
}

// Filter decides whether a test path ("suite/case") runs.
type Filter func(path string) bool

// Runner executes a discovered Plan.
type Runner struct {
	Env        *clienv.Env
	GraphFlags *clienv.GraphFlags
	Kubeconfig string
	Context    string
	Sinks      []Sink
	Filter     Filter
	// Update rewrites case snapshot directories instead of comparing
	// them.
	Update bool
}

// Summary tallies terminal events.
type Summary struct {
	Passed  int
	Failed  int
	Skipped int
}

// Total returns the number of cases that produced a terminal event.
func (s Summary) Total() int { return s.Passed + s.Failed + s.Skipped }

// Run executes every case in the plan sequentially.
func (r *Runner) Run(p *discover.Plan) Summary {
	var sum Summary
	for _, s := range p.Suites {
		if s.Err != nil {
			ev := Event{Suite: s.Name, Path: s.Path(), Action: ActionFail,
				Messages: []string{fmt.Sprintf("suite error: %s", s.Err)}}
			r.emit(ev)
			sum.Failed++
			continue
		}
		r.runSuite(s, &sum)
	}
	return sum
}

func (r *Runner) runSuite(s *discover.Suite, sum *Summary) {
	var graph *krograph.Graph
	var graphErr error
	graphBuilt := false
	getGraph := func() (*krograph.Graph, error) {
		if !graphBuilt {
			graphBuilt = true
			graph, graphErr = r.buildGraph(s)
		}
		return graph, graphErr
	}

	for _, c := range s.Cases {
		path := c.Path(s)
		if r.Filter != nil && !r.Filter(path) {
			continue
		}
		r.emit(Event{Suite: s.Name, Case: c.Name, Path: path, File: c.File, Action: ActionRun})

		start := time.Now()
		ev := Event{Suite: s.Name, Case: c.Name, Path: path, File: c.File}
		switch {
		case c.Err != nil:
			ev.Action = ActionFail
			ev.Messages = []string{fmt.Sprintf("fixture error: %s", c.Err)}
		case c.Skip != "":
			ev.Action = ActionSkip
			ev.Messages = []string{c.Skip}
		default:
			ev.Action, ev.Messages, ev.Info = r.runCase(getGraph, s, c)
		}
		ev.Elapsed = time.Since(start)
		r.emit(ev)

		switch ev.Action {
		case ActionPass:
			sum.Passed++
		case ActionFail:
			sum.Failed++
		case ActionSkip:
			sum.Skipped++
		}
	}
}

// buildGraph loads the suite's RGD and builds its graph once per suite.
func (r *Runner) buildGraph(s *discover.Suite) (*krograph.Graph, error) {
	rgdf, err := rgd.LoadFile(s.RGD, nil)
	if err != nil {
		return nil, fmt.Errorf("loading RGD: %w", err)
	}
	gf := &clienv.GraphFlags{FromCluster: r.GraphFlags != nil && r.GraphFlags.FromCluster}
	if r.GraphFlags != nil {
		gf.CRDs = append(gf.CRDs, r.GraphFlags.CRDs...)
	}
	gf.CRDs = append(gf.CRDs, s.CRDs...)
	g, err := r.Env.BuildGraph(rgdf, gf, r.Kubeconfig, r.Context)
	if err != nil {
		return nil, fmt.Errorf("building graph: %w", err)
	}
	return g, nil
}

// runCase runs the case's single step, checks its expectations and
// evaluates its snapshot directory. The third result holds informational
// lines such as snapshot writes.
func (r *Runner) runCase(getGraph func() (*krograph.Graph, error), s *discover.Suite, c *discover.Case) (Action, []string, []string) {
	step := c.Steps[0]
	g, err := getGraph()
	var res *simulate.Result
	if err == nil {
		res, err = simulate.Run(g, step.Instance.DeepCopy(), step.Observed)
	}

	if ee := errorExpect(step.Expect); ee != nil {
		if msgs := expectError(ee, err); len(msgs) > 0 {
			return ActionFail, msgs, nil
		}
		return ActionPass, nil, nil
	}
	if err != nil {
		return ActionFail, []string{fmt.Sprintf("run failed: %s", err)}, nil
	}
	msgs := checkExpectations(step.Expect, res)
	msgs = append(msgs, checkAssertions(step, res)...)
	var info []string
	if !c.SnapshotOff {
		var fails []string
		fails, info = snapshot.Eval(c.SnapshotDir,
			snapshot.Settings{Mode: s.SnapshotMode, Ignore: s.SnapshotIgnore},
			res, r.Update)
		msgs = append(msgs, fails...)
	}
	if len(msgs) > 0 {
		return ActionFail, msgs, info
	}
	return ActionPass, nil, info
}

func errorExpect(e *spec.Expect) *spec.ErrorExpect {
	if e == nil {
		return nil
	}
	return e.Error
}

// expectError verifies an expected-error assertion against the run error.
// It returns the failure messages (empty = assertion held).
func expectError(ee *spec.ErrorExpect, runErr error) []string {
	fail := func(format string, args ...interface{}) []string {
		return []string{fmt.Sprintf(format, args...)}
	}
	if runErr == nil {
		switch {
		case ee.Contains != "":
			return fail("expected error containing %q, but the run succeeded", ee.Contains)
		default:
			return fail("expected error matching %q, but the run succeeded", ee.Regex)
		}
	}
	var msgs []string
	if ee.Contains != "" && !strings.Contains(runErr.Error(), ee.Contains) {
		msgs = append(msgs, fmt.Sprintf("expected error containing %q, got %q", ee.Contains, runErr.Error()))
	}
	if ee.Regex != "" {
		re, err := regexp.Compile(ee.Regex)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("expect.error.regex: %s", err))
		} else if !re.MatchString(runErr.Error()) {
			msgs = append(msgs, fmt.Sprintf("expected error matching %q, got %q", ee.Regex, runErr.Error()))
		}
	}
	return msgs
}

// checkExpectations compares the simulation result against the declared
// node, status and unmatchedObserved expectations.
func checkExpectations(e *spec.Expect, res *simulate.Result) []string {
	if e == nil {
		return nil
	}
	var msgs []string

	byID := make(map[string]simulate.NodeResult, len(res.Nodes))
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	ids := make([]string, 0, len(e.Nodes))
	for id := range e.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		want := e.Nodes[id]
		got, ok := byID[id]
		if !ok {
			msgs = append(msgs, fmt.Sprintf("nodes.%s: no such node in result", id))
			continue
		}
		if want.State != "" && string(got.State) != want.State {
			msgs = append(msgs, fmt.Sprintf("nodes.%s.state: expected %s, got %s", id, want.State, got.State))
		}
		if want.Count != nil && len(got.Objects) != *want.Count {
			msgs = append(msgs, fmt.Sprintf("nodes.%s: expected %d object(s), got %d", id, *want.Count, len(got.Objects)))
		}
		for i, wantObj := range want.Objects {
			path := fmt.Sprintf("nodes.%s.objects[%d]", id, i)
			if i >= len(got.Objects) {
				msgs = append(msgs, fmt.Sprintf("%s: node produced only %d object(s)", path, len(got.Objects)))
				continue
			}
			msgs = append(msgs, match.Subset(wantObj, got.Objects[i].Object, path)...)
		}
	}

	if e.Status != nil {
		status, found, _ := unstructured.NestedMap(res.Instance.Object, "status")
		if !found {
			msgs = append(msgs, "status: instance has no computed status")
		} else {
			msgs = append(msgs, match.Subset(e.Status, status, "status")...)
		}
	}

	if e.UnmatchedObserved != nil && len(res.UnmatchedObserved) != *e.UnmatchedObserved {
		var names []string
		for _, o := range res.UnmatchedObserved {
			names = append(names, fmt.Sprintf("%s %s/%s", o.GroupVersionKind().String(), o.GetNamespace(), o.GetName()))
		}
		sort.Strings(names)
		msgs = append(msgs, fmt.Sprintf("unmatchedObserved: expected %d, got %d (%s)",
			*e.UnmatchedObserved, len(res.UnmatchedObserved), strings.Join(names, ", ")))
	}
	return msgs
}

func (r *Runner) emit(ev Event) {
	for _, s := range r.Sinks {
		s.Event(ev)
	}
}
