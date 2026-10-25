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

package instance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func testCRD() *apiextensionsv1.CustomResourceDefinition {
	min := float64(1)
	def := json.RawMessage(`"default-sa"`)
	maxLen := int64(10)
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "webapps.kro.run"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "kro.run",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind: "WebApp", Plural: "webapps", Singular: "webapp",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1alpha1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:     "object",
						Required: []string{"spec"},
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {
								Type:     "object",
								Required: []string{"name"},
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"name":           {Type: "string", MaxLength: &maxLen},
									"mode":           {Type: "string", Enum: []apiextensionsv1.JSON{{Raw: json.RawMessage(`"Auto"`)}, {Raw: json.RawMessage(`"Manual"`)}}},
									"replicas":       {Type: "integer", Minimum: &min},
									"serviceAccount": {Type: "string", Default: &apiextensionsv1.JSON{Raw: def}},
								},
								XValidations: apiextensionsv1.ValidationRules{
									{Rule: "self.replicas >= 2", Message: "replicas must be at least 2"},
								},
							},
						},
					},
				},
			}},
		},
	}
}

func obj(spec map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"spec": spec}
}

func fieldMessages(errs field.ErrorList) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

func TestValidate(t *testing.T) {
	s, err := NewSchema(testCRD(), "")
	require.NoError(t, err)
	ctx := context.Background()

	tests := []struct {
		name       string
		obj        map[string]interface{}
		wantErrSub []string
	}{
		{
			name: "valid",
			obj:  obj(map[string]interface{}{"name": "app", "replicas": float64(3)}),
		},
		{
			name:       "unknown field",
			obj:        obj(map[string]interface{}{"name": "app", "bogus": 1, "replicas": float64(3)}),
			wantErrSub: []string{`unknown field "spec.bogus"`},
		},
		{
			name:       "wrong type",
			obj:        obj(map[string]interface{}{"name": "app", "replicas": "three"}),
			wantErrSub: []string{"replicas"},
		},
		{
			name:       "enum violation",
			obj:        obj(map[string]interface{}{"name": "app", "mode": "Nope", "replicas": float64(3)}),
			wantErrSub: []string{"mode"},
		},
		{
			name:       "minimum violation",
			obj:        obj(map[string]interface{}{"name": "app", "replicas": float64(0)}),
			wantErrSub: []string{"replicas"},
		},
		{
			name:       "missing required",
			obj:        obj(map[string]interface{}{"replicas": float64(3)}),
			wantErrSub: []string{"name"},
		},
		{
			name:       "maxLength violation",
			obj:        obj(map[string]interface{}{"name": "this-name-is-way-too-long", "replicas": float64(3)}),
			wantErrSub: []string{"name"},
		},
		{
			name:       "cel rule failure",
			obj:        obj(map[string]interface{}{"name": "app", "replicas": float64(1)}),
			wantErrSub: []string{"replicas must be at least 2"},
		},
		{
			name:       "missing spec",
			obj:        map[string]interface{}{},
			wantErrSub: []string{"spec"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := s.Validate(ctx, tt.obj)
			if len(tt.wantErrSub) == 0 {
				assert.Empty(t, errs, "expected valid, got %v", fieldMessages(errs))
				return
			}
			joined := ""
			for _, m := range fieldMessages(errs) {
				joined += m + "\n"
			}
			for _, sub := range tt.wantErrSub {
				assert.Contains(t, joined, sub)
			}
		})
	}
}

func TestDefault(t *testing.T) {
	s, err := NewSchema(testCRD(), "")
	require.NoError(t, err)

	o := obj(map[string]interface{}{"name": "app"})
	s.Default(o)
	spec := o["spec"].(map[string]interface{})
	assert.Equal(t, "default-sa", spec["serviceAccount"])
}

func TestValidateDoesNotMutateInput(t *testing.T) {
	s, err := NewSchema(testCRD(), "")
	require.NoError(t, err)
	o := obj(map[string]interface{}{"name": "app", "replicas": float64(3)})
	_ = s.Validate(context.Background(), o)
	assert.NotContains(t, o["spec"].(map[string]interface{}), "serviceAccount")
}

func TestClusterScoped(t *testing.T) {
	s, err := NewSchema(testCRD(), "")
	require.NoError(t, err)
	assert.False(t, s.ClusterScoped())

	crd := testCRD()
	crd.Spec.Scope = apiextensionsv1.ClusterScoped
	s, err = NewSchema(crd, "")
	require.NoError(t, err)
	assert.True(t, s.ClusterScoped())
}

func TestNewSchemaVersionSelection(t *testing.T) {
	crd := testCRD()
	s, err := NewSchema(crd, "v1alpha1")
	require.NoError(t, err)
	assert.Equal(t, "v1alpha1", s.Version())

	_, err = NewSchema(crd, "v9")
	assert.Error(t, err)
}
