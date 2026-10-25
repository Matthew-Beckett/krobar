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

// Package rgd loads ResourceGraphDefinition files and builds kro graphs.
package rgd

import (
	"fmt"
	"io"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
)

// Load parses a ResourceGraphDefinition from r using non-strict YAML
// unmarshalling (matching upstream kro CLI behaviour). It errors on empty
// input and when kind is set to something other than ResourceGraphDefinition.
func Load(r io.Reader) (*v1alpha1.ResourceGraphDefinition, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read ResourceGraphDefinition: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ResourceGraphDefinition input is empty")
	}

	var rgd v1alpha1.ResourceGraphDefinition
	if err := yaml.Unmarshal(data, &rgd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ResourceGraphDefinition: %w", err)
	}
	if rgd.Kind != "" && rgd.Kind != "ResourceGraphDefinition" {
		return nil, fmt.Errorf("expected kind ResourceGraphDefinition, got %q", rgd.Kind)
	}
	ApplyDefaults(&rgd)
	return &rgd, nil
}

// ApplyDefaults applies the kubebuilder defaults the API server would set on
// a ResourceGraphDefinition, since files loaded from disk never pass through
// admission. These mirror the +kubebuilder:default markers in kro's
// api/v1alpha1: spec.schema.group defaults to "kro.run" and
// spec.schema.scope defaults to Namespaced.
func ApplyDefaults(rgd *v1alpha1.ResourceGraphDefinition) {
	if rgd == nil || rgd.Spec.Schema == nil {
		return
	}
	if rgd.Spec.Schema.Group == "" {
		rgd.Spec.Schema.Group = "kro.run"
	}
	if rgd.Spec.Schema.Scope == "" {
		rgd.Spec.Schema.Scope = v1alpha1.ResourceScopeNamespaced
	}
}

// LoadFile loads a ResourceGraphDefinition from path. The path "-" reads from
// stdin instead of the filesystem.
func LoadFile(path string, stdin io.Reader) (*v1alpha1.ResourceGraphDefinition, error) {
	if path == "-" {
		return Load(stdin)
	}
	f, err := os.Open(path) // #nosec G304 -- path is a user-supplied CLI argument
	if err != nil {
		return nil, fmt.Errorf("failed to read ResourceGraphDefinition file: %w", err)
	}
	defer func() { _ = f.Close() }()
	return Load(f)
}
