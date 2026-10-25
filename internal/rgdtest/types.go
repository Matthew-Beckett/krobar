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

// Package rgdtest implements the RGDTest fixture format used by
// `krobar test`: declarative expectations over a simulated reconciliation.
package rgdtest

import (
	"fmt"
)

// GroupVersion and Kind identify RGDTest documents.
const (
	APIVersion = "krobar.dev/v1alpha1"
	Kind       = "RGDTest"
)

// RGDTest is a single test document.
type RGDTest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec Spec `json:"spec"`
}

// Spec describes what to run and what to expect.
type Spec struct {
	RGD          string                   `json:"rgd"`
	CRDs         []string                 `json:"crds,omitempty"`
	Instance     map[string]interface{}   `json:"instance,omitempty"`
	InstanceFile string                   `json:"instanceFile,omitempty"`
	Observed     []map[string]interface{} `json:"observed,omitempty"`
	Expect       Expect                   `json:"expect,omitempty"`
}

// Expect holds the assertions for a test.
type Expect struct {
	// Error, when set, requires the pipeline to fail with this substring.
	Error string `json:"error,omitempty"`
	// Nodes maps node IDs to per-node expectations.
	Nodes map[string]NodeExpect `json:"nodes,omitempty"`
	// Status is subset-matched against the instance's computed .status.
	Status map[string]interface{} `json:"status,omitempty"`
}

// NodeExpect holds per-node assertions.
type NodeExpect struct {
	State   string                   `json:"state,omitempty"`
	Count   *int                     `json:"count,omitempty"`
	Objects []map[string]interface{} `json:"objects,omitempty"`
}

// Validate checks the required fields and mutual exclusions.
func (t *RGDTest) Validate() error {
	if t.APIVersion != APIVersion || t.Kind != Kind {
		return fmt.Errorf("document is not a %s %s", APIVersion, Kind)
	}
	if t.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if t.Spec.RGD == "" {
		return fmt.Errorf("spec.rgd is required")
	}
	if t.Spec.Instance != nil && t.Spec.InstanceFile != "" {
		return fmt.Errorf("spec.instance and spec.instanceFile are mutually exclusive")
	}
	if t.Spec.Instance == nil && t.Spec.InstanceFile == "" {
		return fmt.Errorf("one of spec.instance or spec.instanceFile is required")
	}
	return nil
}
