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
	"testing"

	"os"
	"path/filepath"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func testCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "Widget",
				Plural:   "widgets",
				Singular: "widget",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"spec": {
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"size": {Type: "integer"},
									},
								},
							},
						},
					},
				},
				{Name: "v1beta1", Served: false, Storage: false},
			},
		},
	}
}

func TestResolverCoreAndCRD(t *testing.T) {
	set, err := NewSet([]*apiextensionsv1.CustomResourceDefinition{testCRD()})
	require.NoError(t, err)

	// Core type resolves from compiled-in schemas.
	cm, err := set.Resolver.ResolveSchema(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
	require.NoError(t, err)
	require.NotNil(t, cm)
	assert.Contains(t, cm.Properties, "metadata")

	// CRD resolves with apiVersion/kind/metadata present (matching the
	// online discovery shape).
	w, err := set.Resolver.ResolveSchema(schema.GroupVersionKind{Group: "example.com", Version: "v1alpha1", Kind: "Widget"})
	require.NoError(t, err)
	for _, k := range []string{"apiVersion", "kind", "metadata", "spec"} {
		assert.Contains(t, w.Properties, k)
	}
	assert.Equal(t, spec.StringOrArray{"string"}, w.Properties["apiVersion"].Type)
	require.Contains(t, w.Properties["metadata"].Properties, "name")

	// Unserved version does not resolve.
	_, err = set.Resolver.ResolveSchema(schema.GroupVersionKind{Group: "example.com", Version: "v1beta1", Kind: "Widget"})
	require.Error(t, err)

	// Unknown GVK gives the actionable error.
	_, err = set.Resolver.ResolveSchema(schema.GroupVersionKind{Group: "x.io", Version: "v1", Kind: "Nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found offline")
	assert.Contains(t, err.Error(), "--crds")
	assert.Contains(t, err.Error(), "--from-cluster")
}

func TestMapperScopesAndPlurals(t *testing.T) {
	set, err := NewSet([]*apiextensionsv1.CustomResourceDefinition{testCRD()})
	require.NoError(t, err)

	cases := []struct {
		gvk       schema.GroupVersionKind
		resource  string
		namespace bool
	}{
		{schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, "configmaps", true},
		{schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, "deployments", true},
		{schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "namespaces", false},
		{schema.GroupVersionKind{Version: "v1", Kind: "PersistentVolume"}, "persistentvolumes", false},
		{schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}, "clusterroles", false},
		{schema.GroupVersionKind{Group: "storage.k8s.io", Version: "v1", Kind: "StorageClass"}, "storageclasses", false},
		{schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}, "customresourcedefinitions", false},
		{schema.GroupVersionKind{Group: "example.com", Version: "v1alpha1", Kind: "Widget"}, "widgets", false},
	}
	for _, c := range cases {
		m, err := set.Mapper.RESTMapping(c.gvk.GroupKind(), c.gvk.Version)
		require.NoError(t, err, c.gvk.String())
		assert.Equal(t, c.resource, m.Resource.Resource, c.gvk.String())
		expected := meta.RESTScopeNamespace
		if !c.namespace {
			expected = meta.RESTScopeRoot
		}
		assert.Equal(t, expected.Name(), m.Scope.Name(), c.gvk.String())
	}
}

func TestLoadDocs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	write("a.yaml", "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\nspec:\n  group: example.com\n  names: {kind: Widget, plural: widgets}\n  scope: Namespaced\n  versions: [{name: v1, served: true, storage: true}]\n")
	write("b.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: ignored\n---\napiVersion: kro.run/v1alpha1\nkind: ResourceGraphDefinition\nmetadata:\n  name: r.kro.run\nspec:\n  schema: {kind: R}\n")
	write("c.txt", "ignored")

	crds, rgds, err := LoadDocs([]string{dir})
	require.NoError(t, err)
	require.Len(t, crds, 1)
	assert.Equal(t, "widgets.example.com", crds[0].Name)
	require.Len(t, rgds, 1)
	assert.Equal(t, "r.kro.run", rgds[0].RGD.Name)
}
