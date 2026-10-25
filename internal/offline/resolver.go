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

// Package offline builds kro graphs without cluster access by serving the
// schemas and REST mappings the kro graph builder needs from compiled-in
// Kubernetes OpenAPI definitions plus user-supplied CRDs.
package offline

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	openapiresolver "k8s.io/apiserver/pkg/cel/openapi/resolver"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"

	kroschema "github.com/kubernetes-sigs/kro/pkg/graph/schema"
)

// openAPISpec is the published Kubernetes OpenAPI v2 document
// (kubernetes/api/openapi-spec/swagger.json at tag v1.36.0) — the same
// document an API server's /openapi/v2 serves, so the offline resolver sees
// the same schemas as kro's online ClientDiscoveryResolver.
//
//go:embed schemas/v1.36.0.json
var openAPISpec []byte

var (
	definitionsOnce sync.Once
	definitions     map[string]common.OpenAPIDefinition
)

// getOpenAPIDefinitions implements common.GetOpenAPIDefinitions over the
// embedded OpenAPI document. Swagger $ref values ("#/definitions/<name>")
// are rewritten to the bare definition names the kube-openapi ecosystem uses
// (spec.MustCreateRef("<name>")).
func getOpenAPIDefinitions(_ common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	definitionsOnce.Do(func() {
		var doc struct {
			Definitions map[string]spec.Schema `json:"definitions"`
		}
		raw := bytes.ReplaceAll(openAPISpec,
			[]byte(`"$ref": "#/definitions/`), []byte(`"$ref": "`))
		if err := json.Unmarshal(raw, &doc); err != nil {
			// The embedded spec is a build-time invariant; failing to parse
			// it means the shipped document was corrupted/truncated.
			panic(fmt.Sprintf("offline: cannot parse embedded OpenAPI document: %v", err))
		}
		definitions = make(map[string]common.OpenAPIDefinition, len(doc.Definitions))
		for name, s := range doc.Definitions {
			definitions[name] = common.OpenAPIDefinition{Schema: s}
		}
	})
	return definitions
}

// Scheme is the compiled-in scheme backing the offline resolver and mapper:
// client-go's built-in types plus apiextensions/v1 (for CRDs themselves).
var Scheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	return s
}()

// crdSchemaResolver serves OpenAPI schemas for CustomResourceDefinitions
// loaded from files. Schemas are augmented with apiVersion/kind/metadata
// properties so they match the shape kro's online ClientDiscoveryResolver
// returns for CRDs.
type crdSchemaResolver struct {
	schemas map[schema.GroupVersionKind]*spec.Schema
}

// ResolveSchema implements openapiresolver.SchemaResolver.
func (r *crdSchemaResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
	if s, ok := r.schemas[gvk]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("schema for %s not found offline; supply its CRD with --crds or use --from-cluster", gvk.String())
}

// newSchemaResolver builds a combined schema resolver: compiled-in core
// Kubernetes types first, then every served version of the given CRDs.
func newSchemaResolver(crds []*apiextensionsv1.CustomResourceDefinition) (openapiresolver.SchemaResolver, error) {
	core := openapiresolver.NewDefinitionsSchemaResolver(getOpenAPIDefinitions, Scheme)

	crdResolver := &crdSchemaResolver{schemas: map[schema.GroupVersionKind]*spec.Schema{}}

	// The online resolver returns CRD schemas augmented with apiVersion,
	// kind and a full ObjectMeta metadata schema. Reuse the core resolver's
	// ObjectMeta shape (from v1/ConfigMap) for parity.
	var metaSchema *spec.Schema
	if cm, err := core.ResolveSchema(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}); err == nil {
		if m, ok := cm.Properties["metadata"]; ok {
			mc := m
			metaSchema = &mc
		}
	}

	for _, crd := range crds {
		for _, v := range crd.Spec.Versions {
			if !v.Served || v.Schema == nil || v.Schema.OpenAPIV3Schema == nil {
				continue
			}
			s, err := kroschema.ConvertJSONSchemaPropsToSpecSchema(v.Schema.OpenAPIV3Schema)
			if err != nil {
				return nil, fmt.Errorf("failed to convert schema for CRD %s version %s: %w", crd.Name, v.Name, err)
			}
			augmentTopLevel(s, metaSchema)
			crdResolver.schemas[schema.GroupVersionKind{
				Group:   crd.Spec.Group,
				Version: v.Name,
				Kind:    crd.Spec.Names.Kind,
			}] = s
		}
	}

	return core.Combine(crdResolver), nil
}

// augmentTopLevel ensures the schema has the apiVersion/kind/metadata
// top-level properties the API server's discovery documents expose for CRDs.
func augmentTopLevel(s *spec.Schema, metaSchema *spec.Schema) {
	if s.Properties == nil {
		s.Properties = map[string]spec.Schema{}
	}
	for _, k := range []string{"apiVersion", "kind"} {
		if _, ok := s.Properties[k]; !ok {
			s.Properties[k] = spec.Schema{
				SchemaProps: spec.SchemaProps{Type: spec.StringOrArray{"string"}},
			}
		}
	}
	// The API server always serves the full ObjectMeta schema for CRDs, even
	// when the CRD declares a bare `metadata` property.
	if metaSchema != nil {
		s.Properties["metadata"] = *metaSchema
	}
}
