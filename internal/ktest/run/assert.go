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
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/Matthew-Beckett/krobar/internal/ktest/celx"
	"github.com/Matthew-Beckett/krobar/internal/ktest/discover"
	"github.com/Matthew-Beckett/krobar/internal/ktest/fieldpath"
	"github.com/Matthew-Beckett/krobar/internal/ktest/match"
	"github.com/Matthew-Beckett/krobar/internal/ktest/spec"
	"github.com/Matthew-Beckett/krobar/internal/simulate"
)

// checkAssertions evaluates a step's assert list and returns the failure
// messages, each prefixed with the assertion's label.
func checkAssertions(step *discover.Step, res *simulate.Result) []string {
	if len(step.Assert) == 0 {
		return nil
	}
	byID := make(map[string]simulate.NodeResult, len(res.Nodes))
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	var msgs []string
	for i := range step.Assert {
		a := &step.Assert[i]
		label := spec.AssertLabel(a.Name, i)
		for _, d := range evalAssertion(a, res, byID) {
			msgs = append(msgs, label+": "+d)
		}
	}
	return msgs
}

func evalAssertion(a *spec.Assertion, res *simulate.Result, byID map[string]simulate.NodeResult) []string {
	var node *simulate.NodeResult
	if a.Node != "" {
		n, ok := byID[a.Node]
		if !ok {
			return []string{fmt.Sprintf("no such node %q", a.Node)}
		}
		node = &n
	}
	var msgs []string
	if a.State != "" && string(node.State) != a.State {
		msgs = append(msgs, fmt.Sprintf("state: expected %s, got %s", a.State, node.State))
	}
	if a.Count != nil && len(node.Objects) != *a.Count {
		msgs = append(msgs, fmt.Sprintf("count: expected %d, got %d", *a.Count, len(node.Objects)))
	}
	if a.Match == nil && a.Equals == nil && len(a.Absent) == 0 && a.CEL == "" {
		return msgs
	}
	subs, smsgs := subjects(a, node, res)
	msgs = append(msgs, smsgs...)
	for _, s := range subs {
		msgs = append(msgs, checkSubject(a, s.obj, s.prefix, res)...)
	}
	return msgs
}

type subject struct {
	obj    interface{}
	prefix string
}

// subjects resolves the assertion's subject object(s) and their message
// path prefixes.
func subjects(a *spec.Assertion, node *simulate.NodeResult, res *simulate.Result) ([]subject, []string) {
	if a.Node == "" {
		return []subject{{res.Instance.Object, "instance"}}, nil
	}
	nodePrefix := func(i int) string {
		return fmt.Sprintf("nodes.%s.objects[%d]", a.Node, i)
	}
	switch {
	case a.Index != nil:
		i := *a.Index
		if i < 0 || i >= len(node.Objects) {
			return nil, []string{fmt.Sprintf("node %q has %d object(s), index %d out of range", a.Node, len(node.Objects), i)}
		}
		return []subject{{node.Objects[i].Object, nodePrefix(i)}}, nil
	case a.Select != nil:
		var subs []subject
		for i, o := range node.Objects {
			if selectorMatches(a.Select, o) {
				subs = append(subs, subject{o.Object, nodePrefix(i)})
			}
		}
		if len(subs) != 1 {
			return nil, []string{fmt.Sprintf("node %q: select matched %d object(s), want exactly 1", a.Node, len(subs))}
		}
		return subs, nil
	case a.Each:
		if len(node.Objects) == 0 {
			return nil, []string{fmt.Sprintf("node %q has 0 object(s); each requires at least one", a.Node)}
		}
		subs := make([]subject, 0, len(node.Objects))
		for i, o := range node.Objects {
			subs = append(subs, subject{o.Object, nodePrefix(i)})
		}
		return subs, nil
	default:
		if len(node.Objects) != 1 {
			return nil, []string{fmt.Sprintf("node %q has %d object(s); use index, select or each", a.Node, len(node.Objects))}
		}
		return []subject{{node.Objects[0].Object, nodePrefix(0)}}, nil
	}
}

func selectorMatches(sel *spec.Selector, o *unstructured.Unstructured) bool {
	if sel.Name != "" && o.GetName() != sel.Name {
		return false
	}
	if sel.Namespace != "" && o.GetNamespace() != sel.Namespace {
		return false
	}
	if sel.Kind != "" && o.GetKind() != sel.Kind {
		return false
	}
	return true
}

// checkSubject applies the value-level checks (path narrowing, match,
// equals, absent, cel) to one resolved subject.
func checkSubject(a *spec.Assertion, obj interface{}, prefix string, res *simulate.Result) []string {
	sub := obj
	if a.Path != "" {
		p, err := fieldpath.Parse(a.Path)
		if err == nil {
			if v, ok := p.Get(sub); ok {
				sub = v
				prefix = joinPath(prefix, p.String())
			} else {
				return []string{fmt.Sprintf("%s: not found", joinPath(prefix, p.String()))}
			}
		} else {
			return []string{fmt.Sprintf("path: %s", err)}
		}
	}
	var msgs []string
	if a.Match != nil {
		msgs = append(msgs, match.Subset(a.Match, sub, prefix)...)
	}
	if a.Equals != nil {
		emsgs := match.Exact(a.Equals, sub, prefix)
		msgs = append(msgs, emsgs...)
		if len(emsgs) > 0 && !match.HasOperators(a.Equals) {
			msgs = append(msgs, "diff (-expected +got):\n"+yamlDiff(a.Equals, sub))
		}
	}
	for _, ap := range a.Absent {
		p, err := fieldpath.Parse(ap)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("absent: %s", err))
			continue
		}
		if v, ok := p.Get(sub); ok {
			msgs = append(msgs, fmt.Sprintf("%s: expected absent, got %v", joinPath(prefix, p.String()), v))
		}
	}
	if a.CEL != "" {
		msgs = append(msgs, evalCEL(a.CEL, sub, res)...)
	}
	return msgs
}

func evalCEL(expr string, self interface{}, res *simulate.Result) []string {
	prog, err := celx.Compile(expr)
	if err != nil {
		return []string{fmt.Sprintf("cel %q: %s", expr, err)}
	}
	ok, err := celx.Eval(prog, celx.Vars{
		Self:     self,
		Instance: res.Instance.Object,
		Status:   statusOf(res),
		Nodes:    nodesVar(res),
	})
	if err != nil {
		return []string{fmt.Sprintf("cel %q: %s", expr, err)}
	}
	if !ok {
		return []string{fmt.Sprintf("cel %q was false", expr)}
	}
	return nil
}

func statusOf(res *simulate.Result) interface{} {
	if s, found, _ := unstructured.NestedMap(res.Instance.Object, "status"); found {
		return s
	}
	return map[string]interface{}{}
}

// nodesVar builds the nodes CEL variable: id -> {state, reason, objects}.
func nodesVar(res *simulate.Result) map[string]interface{} {
	out := make(map[string]interface{}, len(res.Nodes))
	for _, n := range res.Nodes {
		objs := make([]interface{}, 0, len(n.Objects))
		for _, o := range n.Objects {
			objs = append(objs, o.Object)
		}
		out[n.ID] = map[string]interface{}{
			"state":   string(n.State),
			"reason":  n.Reason,
			"objects": objs,
		}
	}
	return out
}

// joinPath appends a selector to a prefix, omitting the dot when the
// selector starts with a bracket.
func joinPath(prefix, sel string) string {
	if strings.HasPrefix(sel, "[") {
		return prefix + sel
	}
	return prefix + "." + sel
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
