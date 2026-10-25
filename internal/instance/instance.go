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

// Package instance validates CustomResource instances against a generated
// CRD using the same machinery the Kubernetes API server uses: structural
// defaulting, pruning (unknown fields reported), OpenAPI schema validation,
// and CEL x-kubernetes-validations rules.
package instance

import (
	"context"
	"fmt"
	"maps"

	apiextensionsinternal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	celschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/pruning"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
)

// Schema is a compiled validator for one CRD version.
type Schema struct {
	crd        *apiextensionsv1.CustomResourceDefinition
	version    *apiextensionsv1.CustomResourceDefinitionVersion
	structural *structuralschema.Structural
	validator  validation.SchemaValidator
	cel        *celschema.Validator
}

// NewSchema compiles the OpenAPI + CEL validators for the given CRD version
// (the version with Storage=true when versionName is empty).
func NewSchema(crd *apiextensionsv1.CustomResourceDefinition, versionName string) (*Schema, error) {
	var version *apiextensionsv1.CustomResourceDefinitionVersion
	for i := range crd.Spec.Versions {
		v := &crd.Spec.Versions[i]
		if versionName != "" && v.Name == versionName {
			version = v
			break
		}
		if versionName == "" && v.Storage {
			version = v
			break
		}
	}
	if version == nil && versionName == "" && len(crd.Spec.Versions) > 0 {
		version = &crd.Spec.Versions[0]
	}
	if version == nil {
		return nil, fmt.Errorf("CRD %s has no version %q", crd.Name, versionName)
	}
	if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
		return nil, fmt.Errorf("CRD %s version %s has no OpenAPI v3 schema", crd.Name, version.Name)
	}

	internal := &apiextensionsinternal.JSONSchemaProps{}
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
		version.Schema.OpenAPIV3Schema, internal, nil); err != nil {
		return nil, fmt.Errorf("failed to convert schema: %w", err)
	}
	structural, err := structuralschema.NewStructural(internal)
	if err != nil {
		return nil, fmt.Errorf("schema for %s/%s is not structural: %w", crd.Name, version.Name, err)
	}
	validator, _, err := validation.NewSchemaValidator(internal)
	if err != nil {
		return nil, fmt.Errorf("failed to build schema validator: %w", err)
	}
	return &Schema{
		crd:        crd,
		version:    version,
		structural: structural,
		validator:  validator,
		cel:        celschema.NewValidator(structural, true, celconfig.PerCallLimit),
	}, nil
}

// Default applies structural-schema defaulting (and null pruning) to obj in
// place, as the API server does on write.
func (s *Schema) Default(obj map[string]interface{}) {
	defaulting.PruneNonNullableNullsWithoutDefaults(obj, s.structural)
	defaulting.Default(obj, s.structural)
}

// Validate validates obj like the API server would on create: defaulting,
// pruning (unknown fields reported as errors), OpenAPI schema validation and
// CEL x-kubernetes-validations. obj is not modified.
func (s *Schema) Validate(ctx context.Context, obj map[string]interface{}) field.ErrorList {
	obj = deepCopy(obj)

	s.Default(obj)

	var errs field.ErrorList

	// Prune unknown fields, reporting each as an error (strict decoding
	// semantics).
	unknown := pruning.PruneWithOptions(obj, s.structural, true,
		structuralschema.UnknownFieldPathOptions{TrackUnknownFieldPaths: true})
	for _, p := range unknown {
		errs = append(errs, field.Invalid(field.NewPath(""), nil, fmt.Sprintf("unknown field %q", p)))
	}

	errs = append(errs, validation.ValidateCustomResource(nil, obj, s.validator)...)

	if s.cel != nil {
		celErrs, _ := s.cel.Validate(ctx, nil, s.structural, obj, nil, celconfig.RuntimeCELCostBudget)
		errs = append(errs, celErrs...)
	}

	return errs
}

// ClusterScoped reports whether the CRD is cluster-scoped.
func (s *Schema) ClusterScoped() bool {
	return s.crd.Spec.Scope == apiextensionsv1.ClusterScoped
}

// CRD returns the CRD this schema was compiled from.
func (s *Schema) CRD() *apiextensionsv1.CustomResourceDefinition {
	return s.crd
}

// Version returns the version this schema validates against.
func (s *Schema) Version() string {
	return s.version.Name
}

func deepCopy(obj map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(obj))
	maps.Copy(cp, obj)
	for k, v := range cp {
		cp[k] = deepCopyValue(v)
	}
	return cp
}

func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return deepCopy(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i := range t {
			out[i] = deepCopyValue(t[i])
		}
		return out
	case float64:
		// JSON decoders in the API server produce int64 for integral
		// numbers; CEL int comparisons fail on float64.
		if t == float64(int64(t)) {
			return int64(t)
		}
		return t
	default:
		return v
	}
}
