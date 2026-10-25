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

package rgdtest

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/objects"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
	"github.com/Matthew-Beckett/krobar/internal/simulate"
)

// Runner executes RGDTest fixtures.
type Runner struct {
	Env        *clienv.Env
	GraphFlags *clienv.GraphFlags
	Kubeconfig string
	Context    string
}

// ParseDoc strict-decodes one YAML document as an RGDTest. It returns nil,
// nil for documents that are not RGDTests.
func ParseDoc(data []byte) (*RGDTest, error) {
	var probe struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := sigsyaml.Unmarshal(data, &probe); err != nil {
		return nil, nil // undecodable docs are not our kind
	}
	if probe.APIVersion != APIVersion || probe.Kind != Kind {
		return nil, nil
	}
	var tc RGDTest
	// Strict decode so misspelled fixture fields fail loudly.
	if err := sigsyaml.UnmarshalStrict(data, &tc); err != nil {
		return nil, fmt.Errorf("invalid RGDTest: %w", err)
	}
	return &tc, nil
}

// Run executes one fixture whose file lives in dir (all relative paths in
// the spec resolve against it). It returns the failure messages; empty means
// the test passed.
func (r *Runner) Run(tc *RGDTest, dir string) []string {
	fail := func(format string, args ...interface{}) []string {
		return []string{fmt.Sprintf(format, args...)}
	}
	if err := tc.Validate(); err != nil {
		return fail("fixture error: %s", err)
	}

	result, runErr := r.execute(tc, dir)

	if tc.Spec.Expect.Error != "" {
		if runErr == nil {
			return fail("expected error containing %q, but the run succeeded", tc.Spec.Expect.Error)
		}
		if !strings.Contains(runErr.Error(), tc.Spec.Expect.Error) {
			return fail("expected error containing %q, got %q", tc.Spec.Expect.Error, runErr.Error())
		}
		return nil
	}
	if runErr != nil {
		return fail("run failed: %s", runErr)
	}
	return r.checkExpectations(tc, result)
}

// resolve returns path joined to dir, keeping absolute paths as-is.
func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

func (r *Runner) execute(tc *RGDTest, dir string) (*simulate.Result, error) {
	rgdf, err := rgd.LoadFile(resolve(dir, tc.Spec.RGD), nil)
	if err != nil {
		return nil, fmt.Errorf("loading RGD: %w", err)
	}

	gf := &clienv.GraphFlags{FromCluster: r.GraphFlags != nil && r.GraphFlags.FromCluster}
	if r.GraphFlags != nil {
		gf.CRDs = append(gf.CRDs, r.GraphFlags.CRDs...)
	}
	for _, c := range tc.Spec.CRDs {
		gf.CRDs = append(gf.CRDs, resolve(dir, c))
	}

	g, err := r.Env.BuildGraph(rgdf, gf, r.Kubeconfig, r.Context)
	if err != nil {
		return nil, fmt.Errorf("building graph: %w", err)
	}

	var inst *unstructured.Unstructured
	switch {
	case tc.Spec.Instance != nil:
		inst = &unstructured.Unstructured{Object: tc.Spec.Instance}
	case tc.Spec.InstanceFile != "":
		docs, err := objects.ReadFile(resolve(dir, tc.Spec.InstanceFile), nil)
		if err != nil {
			return nil, fmt.Errorf("loading instance file: %w", err)
		}
		if len(docs) != 1 {
			return nil, fmt.Errorf("instance file must contain exactly one document, got %d", len(docs))
		}
		inst = docs[0]
	}

	var observed []*unstructured.Unstructured
	for _, o := range tc.Spec.Observed {
		observed = append(observed, &unstructured.Unstructured{Object: o})
	}

	res, err := simulate.Run(g, inst, observed)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (r *Runner) checkExpectations(tc *RGDTest, res *simulate.Result) []string {
	var msgs []string

	byID := make(map[string]simulate.NodeResult, len(res.Nodes))
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	ids := make([]string, 0, len(tc.Spec.Expect.Nodes))
	for id := range tc.Spec.Expect.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		want := tc.Spec.Expect.Nodes[id]
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
			msgs = append(msgs, MatchSubset(wantObj, got.Objects[i].Object, path)...)
		}
	}

	if tc.Spec.Expect.Status != nil {
		status, found, _ := unstructured.NestedMap(res.Instance.Object, "status")
		if !found {
			msgs = append(msgs, "status: instance has no computed status")
		} else {
			msgs = append(msgs, MatchSubset(tc.Spec.Expect.Status, status, "status")...)
		}
	}
	return msgs
}
