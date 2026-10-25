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

package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const rgdDoc = `apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata: {name: x.kro.run}
spec:
  schema:
    apiVersion: v1alpha1
    kind: Mini
    spec: {name: string}
`

const miniInstance = `apiVersion: kro.run/v1alpha1
kind: Mini
metadata: {name: m, namespace: default}
spec: {name: m}
`

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func discoverOne(t *testing.T, paths ...string) *Suite {
	t.Helper()
	plan, err := Discover(paths)
	require.NoError(t, err)
	require.Len(t, plan.Suites, 1)
	return plan.Suites[0]
}

func caseByName(s *Suite, name string) *Case {
	for _, c := range s.Cases {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestExplicitSuiteWithDirAndFileCases(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
metadata:
  name: mysuite
  labels: {team: a}
spec:
  rgd: rgd.yaml
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "a", "case.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestCase
spec:
  instance:
    apiVersion: kro.run/v1alpha1
    kind: Mini
    metadata: {name: m}
  expect:
    nodes: {cm: {state: Ready}}
`)
	write(t, filepath.Join(root, "b", "instance.yaml"), miniInstance)
	write(t, filepath.Join(root, "cases.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestCase
metadata: {name: standalone-1}
spec:
  instanceFile: b/instance.yaml
---
apiVersion: krobar.dev/v1alpha1
kind: TestCase
metadata: {name: standalone-2}
spec:
  skip: not yet
`)

	s := discoverOne(t, root)
	require.NoError(t, s.Err)
	assert.Equal(t, "mysuite", s.Name)
	assert.Equal(t, filepath.Join(root, "rgd.yaml"), s.RGD)
	assert.Equal(t, map[string]string{"team": "a"}, s.Labels)

	var names []string
	for _, c := range s.Cases {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"a", "b", "standalone-1", "standalone-2"}, names)

	a := caseByName(s, "a")
	require.NotNil(t, a)
	require.NoError(t, a.Err)
	assert.Equal(t, filepath.Join(root, "a", "case.yaml"), a.File)
	require.Len(t, a.Steps, 1)
	assert.Equal(t, "m", a.Steps[0].Instance.GetName())
	require.NotNil(t, a.Steps[0].Expect)

	b := caseByName(s, "b")
	require.NotNil(t, b)
	require.NoError(t, b.Err)
	assert.Empty(t, b.File) // pure convention case
	require.Len(t, b.Steps, 1)
	assert.Equal(t, "Mini", b.Steps[0].Instance.GetKind())

	s1 := caseByName(s, "standalone-1")
	require.NotNil(t, s1)
	require.NoError(t, s1.Err)
	assert.Equal(t, filepath.Join(root, "cases.yaml"), s1.File)
	assert.Equal(t, "m", s1.Steps[0].Instance.GetName())

	s2 := caseByName(s, "standalone-2")
	require.NotNil(t, s2)
	assert.Equal(t, "not yet", s2.Skip)
}

func TestImplicitSuite(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(d, "crds", "c.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: c}\n")
	write(t, filepath.Join(d, "tests", "renders", "instance.yaml"), miniInstance)

	s := discoverOne(t, d)
	require.NoError(t, s.Err)
	assert.Equal(t, filepath.Base(d), s.Name)
	assert.Equal(t, filepath.Join(d, "tests"), s.Root)
	assert.Equal(t, filepath.Join(d, "rgd.yaml"), s.RGD)
	assert.Equal(t, []string{filepath.Join(d, "crds")}, s.CRDs)
	require.Len(t, s.Cases, 1)
	assert.Equal(t, "renders", s.Cases[0].Name)
}

func TestImplicitSuiteRequiresExactlyOneRGD(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "a.yaml"), rgdDoc)
	write(t, filepath.Join(d, "b.yaml"), rgdDoc)
	write(t, filepath.Join(d, "tests", "renders", "instance.yaml"), miniInstance)

	plan, err := Discover([]string{d})
	require.NoError(t, err)
	assert.Empty(t, plan.Suites)
}

func TestCasesGlobFilter(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
  cases: ["kept-*"]
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "instance.yaml"), miniInstance)
	write(t, filepath.Join(root, "kept-one", "case.yaml"), "apiVersion: krobar.dev/v1alpha1\nkind: TestCase\nspec: {}\n")
	write(t, filepath.Join(root, "dropped", "case.yaml"), "apiVersion: krobar.dev/v1alpha1\nkind: TestCase\nspec: {}\n")

	s := discoverOne(t, root)
	require.NoError(t, s.Err)
	require.Len(t, s.Cases, 1)
	assert.Equal(t, "kept-one", s.Cases[0].Name)
}

func TestInstancePrecedenceAndAmbiguity(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "instance.yaml"), `apiVersion: kro.run/v1alpha1
kind: Mini
metadata: {name: suite-default}
`)
	// Case-level instance.yaml beats the suite default.
	write(t, filepath.Join(root, "own", "instance.yaml"), `apiVersion: kro.run/v1alpha1
kind: Mini
metadata: {name: case-own}
`)
	// Inline instance AND instance.yaml in the same dir is ambiguous.
	write(t, filepath.Join(root, "amb", "instance.yaml"), miniInstance)
	write(t, filepath.Join(root, "amb", "case.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestCase
spec:
  instance: {apiVersion: kro.run/v1alpha1, kind: Mini}
`)

	s := discoverOne(t, root)
	require.NoError(t, s.Err)

	own := caseByName(s, "own")
	require.NoError(t, own.Err)
	assert.Equal(t, "case-own", own.Steps[0].Instance.GetName())

	amb := caseByName(s, "amb")
	require.Error(t, amb.Err)
	assert.Contains(t, amb.Err.Error(), "ambiguous instance")
}

func TestInstancePatchMergeAndNoMutation(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
  instance:
    apiVersion: kro.run/v1alpha1
    kind: Mini
    metadata: {name: m}
    spec:
      name: m
      replicas: 1
      drop: gone
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "patched", "case.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestCase
spec:
  instancePatch:
    spec:
      replicas: 3
      drop: null
`)
	write(t, filepath.Join(root, "plain", "case.yaml"), "apiVersion: krobar.dev/v1alpha1\nkind: TestCase\nspec: {}\n")

	s := discoverOne(t, root)
	require.NoError(t, s.Err)

	patched := caseByName(s, "patched")
	require.NoError(t, patched.Err)
	spec, _, _ := unstructured.NestedMap(patched.Steps[0].Instance.Object, "spec")
	assert.Equal(t, float64(3), spec["replicas"])
	assert.NotContains(t, spec, "drop")
	assert.Equal(t, "m", spec["name"])

	// The suite default must not be mutated by the other case's patch.
	plain := caseByName(s, "plain")
	require.NoError(t, plain.Err)
	spec, _, _ = unstructured.NestedMap(plain.Steps[0].Instance.Object, "spec")
	assert.Equal(t, "gone", spec["drop"])
	assert.Equal(t, float64(1), spec["replicas"])
}

func TestObservedMerge(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
  instance: {apiVersion: kro.run/v1alpha1, kind: Mini}
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "observed", "cm.yaml"), `apiVersion: v1
kind: ConfigMap
metadata: {name: cm, namespace: default}
data: {from: suite}
`)
	write(t, filepath.Join(root, "c", "case.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestCase
spec:
  observed:
    - apiVersion: v1
      kind: Secret
      metadata: {name: s}
`)
	write(t, filepath.Join(root, "c", "observed", "cm.yaml"), `apiVersion: v1
kind: ConfigMap
metadata: {name: cm, namespace: default}
data: {from: case}
`)

	s := discoverOne(t, root)
	require.NoError(t, s.Err)
	c := caseByName(s, "c")
	require.NoError(t, c.Err)
	require.Len(t, c.Steps[0].Observed, 2)
	// First-seen order kept; case object replaces the suite one whole.
	assert.Equal(t, "ConfigMap", c.Steps[0].Observed[0].GetKind())
	data, _, _ := unstructured.NestedMap(c.Steps[0].Observed[0].Object, "data")
	assert.Equal(t, "case", data["from"])
	assert.Equal(t, "Secret", c.Steps[0].Observed[1].GetKind())
}

func TestDuplicateCaseName(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
  instance: {apiVersion: kro.run/v1alpha1, kind: Mini}
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "dup", "case.yaml"), "apiVersion: krobar.dev/v1alpha1\nkind: TestCase\nspec: {}\n")
	write(t, filepath.Join(root, "z.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestCase
metadata: {name: dup}
spec: {}
`)

	s := discoverOne(t, root)
	require.NoError(t, s.Err)
	require.Len(t, s.Cases, 2)
	assert.NoError(t, s.Cases[0].Err)
	require.Error(t, s.Cases[1].Err)
	assert.Contains(t, s.Cases[1].Err.Error(), "duplicate case name")
}

func TestMissingInstance(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec: {rgd: rgd.yaml}
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "bare", "case.yaml"), "apiVersion: krobar.dev/v1alpha1\nkind: TestCase\nspec: {}\n")

	s := discoverOne(t, root)
	require.NoError(t, s.Err)
	c := caseByName(s, "bare")
	require.Error(t, c.Err)
	assert.Contains(t, c.Err.Error(), "no instance")
}

func TestRootObservedDirIsNotACase(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
  instance: {apiVersion: kro.run/v1alpha1, kind: Mini}
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "observed", "cm.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: cm}\n")
	write(t, filepath.Join(root, "real", "case.yaml"), "apiVersion: krobar.dev/v1alpha1\nkind: TestCase\nspec: {}\n")

	s := discoverOne(t, root)
	require.NoError(t, s.Err)
	require.Len(t, s.Cases, 1)
	assert.Equal(t, "real", s.Cases[0].Name)
}

func TestRGDTestInSuiteRootIsSuiteError(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
  instance: {apiVersion: kro.run/v1alpha1, kind: Mini}
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "old.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: RGDTest
metadata: {name: legacy}
spec: {}
`)

	s := discoverOne(t, root)
	require.Error(t, s.Err)
	assert.Contains(t, s.Err.Error(), "RGDTest documents are no longer supported")
}

func TestManifestFilePathArg(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), `apiVersion: krobar.dev/v1alpha1
kind: TestSuite
spec:
  rgd: rgd.yaml
  instance: {apiVersion: kro.run/v1alpha1, kind: Mini}
`)
	write(t, filepath.Join(root, "rgd.yaml"), rgdDoc)
	write(t, filepath.Join(root, "c", "case.yaml"), "apiVersion: krobar.dev/v1alpha1\nkind: TestCase\nspec: {}\n")

	s := discoverOne(t, filepath.Join(root, "krobar-test.yaml"))
	require.NoError(t, s.Err)
	assert.Len(t, s.Cases, 1)

	// A non-manifest file argument is an error.
	write(t, filepath.Join(root, "plain.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n")
	_, err := Discover([]string{filepath.Join(root, "plain.yaml")})
	assert.Error(t, err)

	// A nonexistent path argument is an error.
	_, err = Discover([]string{filepath.Join(root, "nope")})
	assert.Error(t, err)
}

func TestBadManifestIsSuiteError(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "krobar-test.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n")

	s := discoverOne(t, root)
	require.Error(t, s.Err)
}
