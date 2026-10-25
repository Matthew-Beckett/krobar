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
	"fmt"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/graph"

	"github.com/Matthew-Beckett/krobar/internal/offline"
)

type offlineGraphBuilder struct {
	set *offline.Set
}

// NewOfflineGraphBuilder returns a GraphBuilder that resolves schemas and
// REST mappings entirely offline from the given offline.Set — no cluster
// access is performed.
func NewOfflineGraphBuilder(set *offline.Set) GraphBuilder {
	return &offlineGraphBuilder{set: set}
}

func (b *offlineGraphBuilder) Build(rgd *v1alpha1.ResourceGraphDefinition) (*graph.Graph, error) {
	builder, err := offline.NewSetBuilder(b.set)
	if err != nil {
		return nil, fmt.Errorf("failed to create offline graph builder: %w", err)
	}

	g, err := builder.NewResourceGraphDefinition(rgd, DefaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource graph definition: %w", err)
	}
	return g, nil
}
