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

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKinds(t *testing.T) {
	doc, err := Parse([]byte(`
apiVersion: krobar.dev/v1alpha1
kind: TestSuite
metadata: {name: s}
spec: {rgd: ./r.yaml}
`))
	require.NoError(t, err)
	s, ok := doc.(*Suite)
	require.True(t, ok)
	assert.Equal(t, "s", s.Metadata.Name)
	assert.Equal(t, "./r.yaml", s.Spec.RGD)

	doc, err = Parse([]byte(`
apiVersion: krobar.dev/v1alpha1
kind: TestCase
metadata: {name: c}
spec:
  skip: later
`))
	require.NoError(t, err)
	c, ok := doc.(*Case)
	require.True(t, ok)
	assert.Equal(t, "c", c.Metadata.Name)
	assert.Equal(t, "later", c.Spec.Skip)

	// Unrelated documents are ignored.
	doc, err = Parse([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n"))
	require.NoError(t, err)
	assert.Nil(t, doc)

	// Undecodable documents are ignored.
	doc, err = Parse([]byte("\t\n"))
	require.NoError(t, err)
	assert.Nil(t, doc)
}

func TestParseRejectsRGDTest(t *testing.T) {
	doc, err := Parse([]byte(`
apiVersion: krobar.dev/v1alpha1
kind: RGDTest
metadata: {name: old}
spec: {rgd: ./r.yaml}
`))
	assert.Nil(t, doc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RGDTest documents are no longer supported")
}

func TestStrictDecode(t *testing.T) {
	doc, err := Parse([]byte(`
apiVersion: krobar.dev/v1alpha1
kind: TestCase
metadata: {name: x}
spec:
  bogusField: 1
`))
	assert.Nil(t, doc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid TestCase")

	doc, err = Parse([]byte(`
apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: ./r.yaml
  notAField: true
`))
	assert.Nil(t, doc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid TestSuite")
}

func TestValidate(t *testing.T) {
	intPtr := func(i int) *int { return &i }
	for _, tc := range []struct {
		name    string
		case_   *Case
		wantErr string
	}{
		{
			name:  "empty spec valid",
			case_: &Case{},
		},
		{
			name: "instance and instanceFile exclusive",
			case_: &Case{Spec: CaseSpec{
				Instance:     map[string]interface{}{"a": 1},
				InstanceFile: "i.yaml",
			}},
			wantErr: "mutually exclusive",
		},
		{
			name: "bad node state",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Nodes: map[string]NodeExpect{"n": {State: "Bogus"}},
			}}},
			wantErr: "nodes.n.state",
		},
		{
			name: "valid node states",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Nodes: map[string]NodeExpect{
					"a": {State: "Ready"}, "b": {State: "NotReady"},
					"c": {State: "Pending"}, "d": {State: "Ignored"},
					"e": {Count: intPtr(0)},
				},
			}}},
		},
		{
			name: "error requires contains or regex",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Error: &ErrorExpect{},
			}}},
			wantErr: "contains or regex",
		},
		{
			name: "bad regex",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Error: &ErrorExpect{Regex: "([unclosed"},
			}}},
			wantErr: "expect.error.regex",
		},
		{
			name: "error exclusive with nodes",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Error: &ErrorExpect{Contains: "x"},
				Nodes: map[string]NodeExpect{"n": {}},
			}}},
			wantErr: "mutually exclusive",
		},
		{
			name: "error exclusive with status",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Error:  &ErrorExpect{Contains: "x"},
				Status: map[string]interface{}{"a": 1},
			}}},
			wantErr: "mutually exclusive",
		},
		{
			name: "error exclusive with unmatchedObserved",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Error:             &ErrorExpect{Contains: "x"},
				UnmatchedObserved: intPtr(0),
			}}},
			wantErr: "mutually exclusive",
		},
		{
			name: "error with both matchers valid",
			case_: &Case{Spec: CaseSpec{Expect: &Expect{
				Error: &ErrorExpect{Contains: "x", Regex: "x+"},
			}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.case_.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestAssertValidate(t *testing.T) {
	intPtr := func(i int) *int { return &i }
	for _, tc := range []struct {
		name    string
		a       Assertion
		wantErr string
	}{
		{
			name:    "empty assertion",
			a:       Assertion{},
			wantErr: "at least one of",
		},
		{name: "state on node ok", a: Assertion{Node: "n", State: "Ready"}},
		{
			name:    "state without node",
			a:       Assertion{State: "Ready"},
			wantErr: "require node",
		},
		{name: "count on node ok", a: Assertion{Node: "n", Count: intPtr(1)}},
		{
			name:    "count without node",
			a:       Assertion{Count: intPtr(1)},
			wantErr: "require node",
		},
		{name: "index ok", a: Assertion{Node: "n", Index: intPtr(0), Match: map[string]interface{}{"a": 1}}},
		{
			name:    "index without node",
			a:       Assertion{Index: intPtr(0), Match: map[string]interface{}{"a": 1}},
			wantErr: "require node",
		},
		{
			name:    "index and each exclusive",
			a:       Assertion{Node: "n", Index: intPtr(0), Each: true, Match: map[string]interface{}{"a": 1}},
			wantErr: "mutually exclusive",
		},
		{
			name:    "select and each exclusive",
			a:       Assertion{Node: "n", Select: &Selector{Name: "x"}, Each: true, Match: map[string]interface{}{"a": 1}},
			wantErr: "mutually exclusive",
		},
		{
			name:    "empty selector",
			a:       Assertion{Node: "n", Select: &Selector{}, Match: map[string]interface{}{"a": 1}},
			wantErr: "select requires at least one field",
		},
		{
			name:    "bad state",
			a:       Assertion{Node: "n", State: "Nope"},
			wantErr: "state:",
		},
		{
			name:    "bad path",
			a:       Assertion{Node: "n", Path: "a..b", Match: map[string]interface{}{"x": 1}},
			wantErr: "path:",
		},
		{
			name:    "bad absent path",
			a:       Assertion{Absent: []string{"a["}},
			wantErr: "absent:",
		},
		{
			name:    "bad match operator",
			a:       Assertion{Match: map[string]interface{}{"a": map[string]interface{}{"$bogus": 1}}},
			wantErr: "match",
		},
		{
			name:    "bad equals operator",
			a:       Assertion{Equals: map[string]interface{}{"a": map[string]interface{}{"$any": "no"}}},
			wantErr: "equals",
		},
		{
			name:    "bad cel",
			a:       Assertion{CEL: "self."},
			wantErr: "cel:",
		},
		{name: "instance subject match", a: Assertion{Match: map[string]interface{}{"a": 1}}},
		{name: "instance subject cel", a: Assertion{CEL: "self != null"}},
		{name: "each ok", a: Assertion{Node: "n", Each: true, Match: map[string]interface{}{"a": 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Case{Spec: CaseSpec{Assert: []Assertion{tc.a}}}
			err := c.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Contains(t, err.Error(), "assert[0]")
			}
		})
	}
}

func TestAssertNotAllowedWithExpectError(t *testing.T) {
	c := &Case{Spec: CaseSpec{
		Expect: &Expect{Error: &ErrorExpect{Contains: "x"}},
		Assert: []Assertion{{Name: "n", Match: map[string]interface{}{"a": 1}}},
	}}
	err := c.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expect.error")
}

func TestSuiteValidate(t *testing.T) {
	s := &Suite{}
	assert.NoError(t, s.Validate())
	s.Spec.Instance = map[string]interface{}{}
	s.Spec.InstanceFile = "i.yaml"
	assert.Error(t, s.Validate())
}

func TestSnapshotValidate(t *testing.T) {
	s := &Suite{Spec: SuiteSpec{Snapshot: &SnapshotSpec{Mode: "partial", Ignore: []string{"a.b[0]"}}}}
	assert.NoError(t, s.Validate())
	s.Spec.Snapshot.Mode = "lax"
	err := s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.snapshot.mode")
	s.Spec.Snapshot = &SnapshotSpec{Ignore: []string{"a..b"}}
	err = s.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.snapshot.ignore")

	off := "off"
	dir := "snaps"
	empty := ""
	for _, tc := range []struct {
		snap  *string
		valid bool
	}{
		{nil, true},
		{&off, true},
		{&dir, true},
		{&empty, false},
	} {
		c := &Case{Spec: CaseSpec{Snapshot: tc.snap}}
		if tc.valid {
			assert.NoError(t, c.Validate())
		} else {
			assert.Error(t, c.Validate())
		}
	}
}
