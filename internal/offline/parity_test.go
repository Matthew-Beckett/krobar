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

package offline

import (
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/kube-openapi/pkg/validation/spec"

	kroresolver "github.com/kubernetes-sigs/kro/pkg/graph/schema/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Matthew-Beckett/krobar/internal/kube"
)

// TestLiveShapeParity compares the offline resolver's CRD schema shape with
// the online ClientDiscoveryResolver for a CRD installed on the cluster.
// Run with KROBAR_LIVE_TEST=1.
func TestLiveShapeParity(t *testing.T) {
	if os.Getenv("KROBAR_LIVE_TEST") == "" {
		t.Skip("set KROBAR_LIVE_TEST=1 to run against a cluster")
	}
	crdName := os.Getenv("KROBAR_LIVE_CRD")
	if crdName == "" {
		crdName = "clusterissuers.cert-manager.io"
	}
	out, err := exec.Command("kubectl", "get", "crd", crdName, "-o", "json").Output() // #nosec -- test-only read of a user-selected CRD
	require.NoError(t, err)
	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, json.Unmarshal(out, &crd))

	cfg, err := kube.RESTConfig("", "")
	require.NoError(t, err)
	httpClient, err := rest.HTTPClientFor(cfg)
	require.NoError(t, err)
	online, err := kroresolver.NewCombinedResolver(cfg, httpClient)
	require.NoError(t, err)
	set, err := NewSet([]*apiextensionsv1.CustomResourceDefinition{&crd})
	require.NoError(t, err)

	for _, v := range crd.Spec.Versions {
		if !v.Served {
			continue
		}
		gvk := schema.GroupVersionKind{Group: crd.Spec.Group, Version: v.Name, Kind: crd.Spec.Names.Kind}
		on, err := online.ResolveSchema(gvk)
		require.NoError(t, err, "online resolve %v", gvk)
		off, err := set.Resolver.ResolveSchema(gvk)
		require.NoError(t, err, "offline resolve %v", gvk)

		onKeys := sortedKeys(on.Properties)
		offKeys := sortedKeys(off.Properties)
		assert.Subset(t, offKeys, onKeys, "%v: online top-level props not in offline", gvk)
		for _, k := range []string{"apiVersion", "kind", "metadata"} {
			require.Contains(t, offKeys, k, "%v missing %s", gvk, k)
		}
		offMeta := sortedKeys(off.Properties["metadata"].Properties)
		if onMeta := sortedKeys(on.Properties["metadata"].Properties); len(onMeta) > 0 {
			assert.Subset(t, offMeta, onMeta, "%v metadata props", gvk)
		}
		for _, k := range []string{"name", "namespace", "labels", "annotations"} {
			require.Contains(t, offMeta, k, "%v metadata missing %s", gvk, k)
		}
	}
}

func sortedKeys(m map[string]spec.Schema) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
