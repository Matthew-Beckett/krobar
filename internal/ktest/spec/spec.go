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

// Package spec defines the krobar test document kinds: TestSuite (a
// directory manifest, krobar-test.yaml) and TestCase (a case.yaml or a
// document in a standalone test file).
package spec

import (
	"fmt"
	"regexp"

	sigsyaml "sigs.k8s.io/yaml"

	"github.com/Matthew-Beckett/krobar/internal/ktest/celx"
	"github.com/Matthew-Beckett/krobar/internal/ktest/fieldpath"
	"github.com/Matthew-Beckett/krobar/internal/ktest/match"
)

// GroupVersion and kinds recognised by Parse.
const (
	APIVersion = "krobar.dev/v1alpha1"
	KindSuite  = "TestSuite"
	KindCase   = "TestCase"
	legacyKind = "RGDTest"
)

// Metadata is the object metadata common to both kinds.
type Metadata struct {
	Name   string            `json:"name,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Suite is a TestSuite document: the manifest of a test suite directory.
type Suite struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Metadata   Metadata  `json:"metadata"`
	Spec       SuiteSpec `json:"spec"`
}

// SuiteSpec configures a suite: which RGD to build, which cases to run,
// and the shared default instance.
type SuiteSpec struct {
	// RGD is the ResourceGraphDefinition file, relative to the suite root.
	RGD string `json:"rgd,omitempty"`
	// CRDs holds extra CRD files/dirs, relative to the suite root.
	CRDs []string `json:"crds,omitempty"`
	// Cases optionally restricts which entries in the suite root are cases
	// (filepath.Match globs against entry names); empty means all.
	Cases []string `json:"cases,omitempty"`
	// Instance is the shared default instance (XOR InstanceFile).
	Instance map[string]interface{} `json:"instance,omitempty"`
	// InstanceFile points at a file holding the shared default instance,
	// relative to the suite root (XOR Instance).
	InstanceFile string `json:"instanceFile,omitempty"`
	// Snapshot configures snapshot comparison for the suite's cases.
	Snapshot *SnapshotSpec `json:"snapshot,omitempty"`
}

// SnapshotSpec configures per-case snapshot comparison of rendered
// objects against files under an expected directory.
type SnapshotSpec struct {
	// Mode is strict, partial or off; empty means strict.
	Mode string `json:"mode,omitempty"`
	// Ignore lists field paths stripped from rendered objects and the
	// instance status before comparison and before writing.
	Ignore []string `json:"ignore,omitempty"`
}

// Case is a TestCase document.
type Case struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       CaseSpec `json:"spec"`
}

// CaseSpec describes one case.
type CaseSpec struct {
	// Skip, when non-empty, marks the case skipped with this reason.
	Skip string `json:"skip,omitempty"`
	// Instance overrides the suite instance (XOR InstanceFile).
	Instance map[string]interface{} `json:"instance,omitempty"`
	// InstanceFile points at the case instance, relative to the case
	// file's directory (XOR Instance).
	InstanceFile string `json:"instanceFile,omitempty"`
	// InstancePatch is an RFC 7386 merge patch applied over the resolved
	// base instance.
	InstancePatch map[string]interface{} `json:"instancePatch,omitempty"`
	// Observed adds inline observed objects.
	Observed []map[string]interface{} `json:"observed,omitempty"`
	// Expect holds the assertions; nil means the case passes when the run
	// succeeds.
	Expect *Expect `json:"expect,omitempty"`
	// Assert holds free-form assertions evaluated after expect.
	Assert []Assertion `json:"assert,omitempty"`
	// Snapshot is "off" to disable snapshot comparison, or a directory
	// path holding the case's snapshot files (relative to the case
	// file's directory).
	Snapshot *string `json:"snapshot,omitempty"`
}

// Assertion checks one property of the run result: a node's state or
// object count, or a match/exact/absent/CEL check against a resolved
// subject (a node object or the instance).
type Assertion struct {
	// Name labels the assertion in failure messages.
	Name string `json:"name,omitempty"`
	// Node selects a node as subject; empty selects the instance object
	// (including its computed .status).
	Node string `json:"node,omitempty"`
	// Index picks one object of the node.
	Index *int `json:"index,omitempty"`
	// Select picks one object of the node by identity fields.
	Select *Selector `json:"select,omitempty"`
	// Each applies the check to every object of the node.
	Each bool `json:"each,omitempty"`
	// Path narrows the subject object before match/equals/absent/cel.
	Path string `json:"path,omitempty"`
	// State requires the node's state.
	State string `json:"state,omitempty"`
	// Count requires this many objects on the node.
	Count *int `json:"count,omitempty"`
	// Match is subset-matched against the subject.
	Match map[string]interface{} `json:"match,omitempty"`
	// Equals is exact-matched against the subject.
	Equals interface{} `json:"equals,omitempty"`
	// Absent lists field paths that must not exist on the subject.
	Absent []string `json:"absent,omitempty"`
	// CEL evaluates the expression with self bound to the subject.
	CEL string `json:"cel,omitempty"`
}

// Selector picks a node object by identity; empty fields match anything.
type Selector struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

// AssertLabel formats an assertion's message prefix.
func AssertLabel(name string, i int) string {
	if name != "" {
		return fmt.Sprintf("assert %q", name)
	}
	return fmt.Sprintf("assert[%d]", i)
}

// Expect holds the assertions for a case.
type Expect struct {
	// Error, when set, requires the run to fail; mutually exclusive with
	// the other fields.
	Error *ErrorExpect `json:"error,omitempty"`
	// Nodes maps node IDs to per-node expectations.
	Nodes map[string]NodeExpect `json:"nodes,omitempty"`
	// Status is subset-matched against the instance's computed .status.
	Status map[string]interface{} `json:"status,omitempty"`
	// UnmatchedObserved asserts how many observed objects matched no node.
	UnmatchedObserved *int `json:"unmatchedObserved,omitempty"`
}

// ErrorExpect matches an expected run error. At least one field is
// required; when both are set, both must hold.
type ErrorExpect struct {
	Contains string `json:"contains,omitempty"`
	Regex    string `json:"regex,omitempty"`
}

// NodeExpect holds per-node assertions.
type NodeExpect struct {
	State   string                   `json:"state,omitempty"`
	Count   *int                     `json:"count,omitempty"`
	Objects []map[string]interface{} `json:"objects,omitempty"`
}

// Node states accepted in NodeExpect.State (mirror simulate.State).
var nodeStates = map[string]bool{
	"Ready":    true,
	"NotReady": true,
	"Pending":  true,
	"Ignored":  true,
}

// Parse decodes one YAML document. It returns a *Suite or *Case for
// documents of those kinds, an error for legacy RGDTest documents and for
// malformed krobar documents, and (nil, nil) for anything else.
func Parse(data []byte) (interface{}, error) {
	var probe struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := sigsyaml.Unmarshal(data, &probe); err != nil {
		return nil, nil // undecodable docs are not our kinds
	}
	if probe.APIVersion != APIVersion {
		return nil, nil
	}
	switch probe.Kind {
	case KindSuite:
		var s Suite
		if err := sigsyaml.UnmarshalStrict(data, &s); err != nil {
			return nil, fmt.Errorf("invalid TestSuite: %w", err)
		}
		return &s, nil
	case KindCase:
		var c Case
		if err := sigsyaml.UnmarshalStrict(data, &c); err != nil {
			return nil, fmt.Errorf("invalid TestCase: %w", err)
		}
		return &c, nil
	case legacyKind:
		return nil, fmt.Errorf("RGDTest documents are no longer supported; convert to a TestSuite directory (see README \"Fixture tests\")")
	default:
		return nil, nil
	}
}

// Validate checks the suite spec's mutual exclusions and snapshot mode.
func (s *Suite) Validate() error {
	if s.Spec.Instance != nil && s.Spec.InstanceFile != "" {
		return fmt.Errorf("spec.instance and spec.instanceFile are mutually exclusive")
	}
	if sn := s.Spec.Snapshot; sn != nil {
		switch sn.Mode {
		case "", "strict", "partial", "off":
		default:
			return fmt.Errorf("spec.snapshot.mode: must be one of strict, partial or off, got %q", sn.Mode)
		}
		for _, p := range sn.Ignore {
			if _, err := fieldpath.Parse(p); err != nil {
				return fmt.Errorf("spec.snapshot.ignore: %w", err)
			}
		}
	}
	return nil
}

// Validate checks the case spec: instance XOR instanceFile, node states,
// and the expect.error rules.
func (c *Case) Validate() error {
	if c.Spec.Instance != nil && c.Spec.InstanceFile != "" {
		return fmt.Errorf("spec.instance and spec.instanceFile are mutually exclusive")
	}
	if e := c.Spec.Expect; e != nil {
		if err := e.validate(); err != nil {
			return err
		}
	}
	if len(c.Spec.Assert) > 0 && c.Spec.Expect != nil && c.Spec.Expect.Error != nil {
		return fmt.Errorf("spec.assert cannot be used together with expect.error")
	}
	for i, a := range c.Spec.Assert {
		if err := a.validate(); err != nil {
			return fmt.Errorf("%s: %w", AssertLabel(a.Name, i), err)
		}
	}
	if c.Spec.Snapshot != nil && *c.Spec.Snapshot == "" {
		return fmt.Errorf(`spec.snapshot: must be "off" or a directory path`)
	}
	return nil
}

func (a *Assertion) validate() error {
	if a.State == "" && a.Count == nil && a.Match == nil &&
		a.Equals == nil && len(a.Absent) == 0 && a.CEL == "" {
		return fmt.Errorf("requires at least one of state, count, match, equals, absent or cel")
	}
	selectors := 0
	for _, set := range []bool{a.Index != nil, a.Select != nil, a.Each} {
		if set {
			selectors++
		}
	}
	if selectors > 1 {
		return fmt.Errorf("index, select and each are mutually exclusive")
	}
	if selectors > 0 && a.Node == "" {
		return fmt.Errorf("index, select and each require node")
	}
	if (a.State != "" || a.Count != nil) && a.Node == "" {
		return fmt.Errorf("state and count require node")
	}
	if a.State != "" && !nodeStates[a.State] {
		return fmt.Errorf("state: must be one of Ready, NotReady, Pending, Ignored, got %q", a.State)
	}
	if a.Select != nil && a.Select.Name == "" && a.Select.Namespace == "" && a.Select.Kind == "" {
		return fmt.Errorf("select requires at least one field")
	}
	if a.Path != "" {
		if _, err := fieldpath.Parse(a.Path); err != nil {
			return fmt.Errorf("path: %w", err)
		}
	}
	for _, p := range a.Absent {
		if _, err := fieldpath.Parse(p); err != nil {
			return fmt.Errorf("absent: %w", err)
		}
	}
	if a.Match != nil {
		if err := match.Validate(a.Match, "match"); err != nil {
			return err
		}
	}
	if a.Equals != nil {
		if err := match.Validate(a.Equals, "equals"); err != nil {
			return err
		}
	}
	if a.CEL != "" {
		if _, err := celx.Compile(a.CEL); err != nil {
			return fmt.Errorf("cel: %w", err)
		}
	}
	return nil
}

func (e *Expect) validate() error {
	for id, n := range e.Nodes {
		if n.State != "" && !nodeStates[n.State] {
			return fmt.Errorf("expect.nodes.%s.state: must be one of Ready, NotReady, Pending, Ignored, got %q", id, n.State)
		}
	}
	if e.Error != nil {
		if len(e.Nodes) > 0 || e.Status != nil || e.UnmatchedObserved != nil {
			return fmt.Errorf("expect.error is mutually exclusive with expect.nodes, expect.status and expect.unmatchedObserved")
		}
		if e.Error.Contains == "" && e.Error.Regex == "" {
			return fmt.Errorf("expect.error requires at least one of contains or regex")
		}
		if e.Error.Regex != "" {
			if _, err := regexp.Compile(e.Error.Regex); err != nil {
				return fmt.Errorf("expect.error.regex: %w", err)
			}
		}
	}
	return nil
}
