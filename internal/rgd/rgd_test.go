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

package rgd

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
)

const examplePath = "../../testdata/webapp-rgd.yaml"

func TestLoadFile(t *testing.T) {
	r, err := LoadFile(examplePath, nil)
	require.NoError(t, err)
	assert.Equal(t, "ResourceGraphDefinition", r.Kind)
	assert.Equal(t, "webapp.kro.run", r.Name)
	require.NotEmpty(t, r.Spec.Resources)
}

func TestLoadFileStdin(t *testing.T) {
	data, err := os.ReadFile(examplePath)
	require.NoError(t, err)

	r, err := LoadFile("-", strings.NewReader(string(data)))
	require.NoError(t, err)
	assert.Equal(t, "webapp.kro.run", r.Name)
}

func TestLoadEmpty(t *testing.T) {
	_, err := Load(strings.NewReader(""))
	assert.ErrorContains(t, err, "empty")
}

func TestLoadWrongKind(t *testing.T) {
	doc := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"
	_, err := Load(strings.NewReader(doc))
	assert.ErrorContains(t, err, "ResourceGraphDefinition")
}

func TestLoadInvalidYAML(t *testing.T) {
	_, err := Load(strings.NewReader("\tnot: [valid"))
	assert.Error(t, err)
}

func TestLoadFileMissing(t *testing.T) {
	_, err := LoadFile("does-not-exist.yaml", nil)
	assert.Error(t, err)
}

func TestApplyDefaults(t *testing.T) {
	t.Run("defaults applied to loaded file", func(t *testing.T) {
		r, err := LoadFile(examplePath, nil)
		require.NoError(t, err)
		require.NotNil(t, r.Spec.Schema)
		assert.Equal(t, "kro.run", r.Spec.Schema.Group)
		assert.Equal(t, v1alpha1.ResourceScopeNamespaced, r.Spec.Schema.Scope)
	})

	t.Run("explicit values preserved", func(t *testing.T) {
		r := &v1alpha1.ResourceGraphDefinition{
			Spec: v1alpha1.ResourceGraphDefinitionSpec{
				Schema: &v1alpha1.Schema{
					Group: "example.com",
					Scope: v1alpha1.ResourceScopeCluster,
				},
			},
		}
		ApplyDefaults(r)
		assert.Equal(t, "example.com", r.Spec.Schema.Group)
		assert.Equal(t, v1alpha1.ResourceScopeCluster, r.Spec.Schema.Scope)
	})

	t.Run("nil schema is a no-op", func(t *testing.T) {
		r := &v1alpha1.ResourceGraphDefinition{}
		ApplyDefaults(r)
		assert.Nil(t, r.Spec.Schema)
		ApplyDefaults(nil)
	})
}
