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

package simulate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"

	krograph "github.com/kubernetes-sigs/kro/pkg/graph"

	"github.com/Matthew-Beckett/krobar/internal/offline"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

const testRGD = `
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
metadata:
  name: simtests.kro.run
spec:
  schema:
    apiVersion: v1alpha1
    kind: SimTest
    spec:
      name: string
      replicas: integer | default=3
      enableExtra: boolean | default=false
      items: '[]string | default=["a","b"]'
    status:
      cmName: ${configmap.metadata.name}
  resources:
    - id: secretref
      externalRef:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: shared-config
    - id: configmap
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${schema.spec.name}-cm
        data:
          key: ${secretref.data.somekey}
      readyWhen:
        - ${configmap.data.ready == "true"}
    - id: extra
      includeWhen:
        - ${schema.spec.enableExtra == true}
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${schema.spec.name}-extra
    - id: pods
      forEach:
        - it: ${schema.spec.items}
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${schema.spec.name}-${it}
`

func buildGraph(t *testing.T) *krograph.Graph {
	t.Helper()
	r, err := rgd.Load(strings.NewReader(testRGD))
	require.NoError(t, err)
	set, err := offline.NewSet(nil)
	require.NoError(t, err)
	g, err := rgd.NewOfflineGraphBuilder(set).Build(r)
	require.NoError(t, err)
	return g
}

func inst(t *testing.T, spec map[string]interface{}) *unstructured.Unstructured {
	t.Helper()
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kro.run/v1alpha1",
		"kind":       "SimTest",
		"metadata":   map[string]interface{}{"name": "t", "namespace": "default"},
		"spec":       spec,
	}}
}

func obj(t *testing.T, yaml string) *unstructured.Unstructured {
	t.Helper()
	m := map[string]interface{}{}
	require.NoError(t, sigsyaml.Unmarshal([]byte(yaml), &m))
	return &unstructured.Unstructured{Object: m}
}

func nodeByID(t *testing.T, res *Result, id string) NodeResult {
	t.Helper()
	for _, n := range res.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node %q in result", id)
	return NodeResult{}
}

func TestPendingWithoutObserved(t *testing.T) {
	g := buildGraph(t)
	res, err := Run(g, inst(t, map[string]interface{}{"name": "t"}), nil)
	require.NoError(t, err)

	external := nodeByID(t, res, "secretref")
	assert.Equal(t, StatePending, external.State)
	assert.Contains(t, external.Reason, "external reference")

	// configmap depends on secretref.data.somekey -> pending transitively.
	cm := nodeByID(t, res, "configmap")
	assert.Equal(t, StatePending, cm.State)

	// includeWhen false -> ignored.
	extra := nodeByID(t, res, "extra")
	assert.Equal(t, StateIgnored, extra.State)

	// forEach over default ["a","b"] -> two objects, ready (no readyWhen).
	pods := nodeByID(t, res, "pods")
	assert.Equal(t, StateReady, pods.State)
	require.Len(t, pods.Objects, 2)

	// Defaults from the SimpleSchema markers were applied.
	replicas, _, _ := unstructured.NestedInt64(res.Instance.Object, "spec", "replicas")
	assert.Equal(t, int64(3), replicas)

	// Instance status depends on configmap -> not resolvable yet.
	_, found, _ := unstructured.NestedString(res.Instance.Object, "status", "cmName")
	assert.False(t, found)
}

func TestObservedOverlayResolvesAndReadiness(t *testing.T) {
	g := buildGraph(t)
	secret := obj(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: default
data:
  somekey: dmFsdWU=
`)
	// The user's observed ConfigMap carries the data.ready field the
	// readyWhen expression needs; the desired object's data.key is preserved.
	cmObserved := obj(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: t-cm
  namespace: default
data:
  ready: "true"
`)
	unmatched := obj(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: something-else
  namespace: default
`)

	res, err := Run(g, inst(t, map[string]interface{}{"name": "t"}),
		[]*unstructured.Unstructured{secret, cmObserved, unmatched})
	require.NoError(t, err)

	assert.Equal(t, StateReady, nodeByID(t, res, "secretref").State)
	assert.Equal(t, StateReady, nodeByID(t, res, "configmap").State)

	// Objects are the desired manifests; the expression-resolved data.key is
	// what would be applied.
	key, _, _ := unstructured.NestedString(nodeByID(t, res, "configmap").Objects[0].Object, "data", "key")
	assert.Equal(t, "dmFsdWU=", key)

	// Instance status now resolves.
	cmName, found, _ := unstructured.NestedString(res.Instance.Object, "status", "cmName")
	assert.True(t, found)
	assert.Equal(t, "t-cm", cmName)

	require.Len(t, res.UnmatchedObserved, 1)
	assert.Equal(t, "something-else", res.UnmatchedObserved[0].GetName())
}

func TestNotReadyViaReadyWhen(t *testing.T) {
	g := buildGraph(t)
	secret := obj(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: default
data:
  somekey: eA==
`)
	// No overlay on the ConfigMap -> data.ready missing -> readyWhen fails.
	res, err := Run(g, inst(t, map[string]interface{}{"name": "t"}),
		[]*unstructured.Unstructured{secret})
	require.NoError(t, err)

	cm := nodeByID(t, res, "configmap")
	assert.Equal(t, StateNotReady, cm.State)
	assert.Contains(t, cm.Reason, "readiness")
}

func TestIncludeWhenToggled(t *testing.T) {
	g := buildGraph(t)
	secret := obj(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared-config
  namespace: default
data:
  somekey: eA==
`)
	res, err := Run(g, inst(t, map[string]interface{}{"name": "t", "enableExtra": true}),
		[]*unstructured.Unstructured{secret})
	require.NoError(t, err)

	extra := nodeByID(t, res, "extra")
	assert.Equal(t, StateReady, extra.State)
	require.Len(t, extra.Objects, 1)
	assert.Equal(t, "t-extra", extra.Objects[0].GetName())
}

func TestInvalidInstanceRejected(t *testing.T) {
	g := buildGraph(t)
	bad := inst(t, map[string]interface{}{
		"name":     "t",
		"replicas": "not-a-number",
	})
	_, err := Run(g, bad, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instance is invalid")
}
