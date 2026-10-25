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
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
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

const miniInstance = `
apiVersion: kro.run/v1alpha1
kind: Mini
metadata:
  name: m
  namespace: default
spec:
  name: m
`

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rgd.yaml"), []byte(miniRGD), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(body), 0o600))
	return dir
}

func TestRunnerPassAndMismatchMessages(t *testing.T) {
	r := &Runner{Env: clienv.Default()}

	fixture := `
apiVersion: krobar.dev/v1alpha1
kind: RGDTest
metadata:
  name: ok
spec:
  rgd: ./rgd.yaml
  instance:
    apiVersion: kro.run/v1alpha1
    kind: Mini
    metadata: {name: m, namespace: default}
    spec: {name: m, replicas: 3}
  expect:
    nodes:
      cm:
        state: Ready
        count: 1
`
	dir := writeFixture(t, fixture)
	docs := loadTestDocs(t, filepath.Join(dir, "test.yaml"))
	require.Empty(t, r.Run(docs[0], dir))

	fail := `
apiVersion: krobar.dev/v1alpha1
kind: RGDTest
metadata:
  name: bad
spec:
  rgd: ./rgd.yaml
  instance:
    apiVersion: kro.run/v1alpha1
    kind: Mini
    metadata: {name: m, namespace: default}
    spec: {name: m}
  expect:
    nodes:
      cm:
        objects:
          - metadata: {name: other-cm}
      ghost:
        state: Ready
`
	dir2 := writeFixture(t, fail)
	docs2 := loadTestDocs(t, filepath.Join(dir2, "test.yaml"))
	msgs := r.Run(docs2[0], dir2)
	assert.Contains(t, msgs, "nodes.cm.objects[0].metadata.name: expected other-cm, got m-cm")
	assert.Contains(t, msgs, "nodes.ghost: no such node in result")
}

func TestRunnerExpectedError(t *testing.T) {
	r := &Runner{Env: clienv.Default()}
	body := `
apiVersion: krobar.dev/v1alpha1
kind: RGDTest
metadata:
  name: expect-error
spec:
  rgd: ./rgd.yaml
  instance:
    apiVersion: kro.run/v1alpha1
    kind: Mini
    metadata: {name: m, namespace: default}
    spec: {name: m, replicas: nope}
  expect:
    error: instance is invalid
`
	dir := writeFixture(t, body)
	docs := loadTestDocs(t, filepath.Join(dir, "test.yaml"))
	require.Empty(t, r.Run(docs[0], dir))
}

func TestStrictDecodeAndValidate(t *testing.T) {
	// Unknown spec field must fail strict decoding.
	doc, err := ParseDoc([]byte(`
apiVersion: krobar.dev/v1alpha1
kind: RGDTest
metadata: {name: x}
spec:
  rgd: ./r.yaml
  instance: {}
  bogusField: 1
`))
	require.Error(t, err)
	assert.Nil(t, doc)
	assert.Contains(t, err.Error(), "invalid RGDTest")

	// instance XOR instanceFile.
	tc := &RGDTest{APIVersion: APIVersion, Kind: Kind}
	tc.Metadata.Name = "x"
	tc.Spec.RGD = "r.yaml"
	tc.Spec.Instance = map[string]interface{}{}
	tc.Spec.InstanceFile = "i.yaml"
	assert.Error(t, tc.Validate())
}

func loadTestDocs(t *testing.T, path string) []*RGDTest {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test file
	require.NoError(t, err)
	var out []*RGDTest
	for _, doc := range bytes.Split(data, []byte("\n---")) {
		tc, err := ParseDoc(doc)
		require.NoError(t, err)
		if tc != nil {
			out = append(out, tc)
		}
	}
	return out
}
