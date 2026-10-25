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

	"k8s.io/client-go/rest"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	kroclient "github.com/kubernetes-sigs/kro/pkg/client"
	"github.com/kubernetes-sigs/kro/pkg/graph"
)

// GraphBuilder builds a processed kro graph from a ResourceGraphDefinition.
// It is a seam so CLI commands can be tested without cluster access.
type GraphBuilder interface {
	// Build processes an RGD and returns the fully built graph.
	Build(rgd *v1alpha1.ResourceGraphDefinition) (*graph.Graph, error)
}

type clusterGraphBuilder struct {
	cfg *rest.Config
}

// NewClusterGraphBuilder returns a GraphBuilder that resolves schemas and
// discovery information from the cluster described by cfg, using
// kroclient.NewSet and graph.NewBuilder like the upstream kro CLI.
func NewClusterGraphBuilder(cfg *rest.Config) GraphBuilder {
	return &clusterGraphBuilder{cfg: cfg}
}

func (b *clusterGraphBuilder) Build(rgd *v1alpha1.ResourceGraphDefinition) (*graph.Graph, error) {
	set, err := kroclient.NewSet(kroclient.Config{RestConfig: b.cfg})
	if err != nil {
		return nil, fmt.Errorf("failed to create client set: %w", err)
	}

	builder, err := graph.NewBuilder(set.RESTConfig(), set.HTTPClient())
	if err != nil {
		return nil, fmt.Errorf("failed to create graph builder: %w", err)
	}

	g, err := builder.NewResourceGraphDefinition(rgd, DefaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource graph definition: %w", err)
	}
	return g, nil
}
