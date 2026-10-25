// Copyright 2025 The Kubernetes Authors.
// Modified for krobar by the Krobar Authors.
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

package generate

import (
	"fmt"
	"slices"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/spf13/cobra"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/graph"
)

func newDiagramCommand(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "diagram [FILE]",
		Short: "Generate a diagram from a ResourceGraphDefinition",
		Long: "Generate a diagram from a ResourceGraphDefinition file. This command reads the " +
			"ResourceGraphDefinition and outputs an interactive HTML diagram.\n\n" +
			"Note: the -o/--format flag is ignored by this command; output is always HTML.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rgdf, err := o.load(cmd, args)
			if err != nil {
				return err
			}
			if err := generateDiagram(cmd, o, rgdf); err != nil {
				return fmt.Errorf("failed to generate diagram: %w", err)
			}
			return nil
		},
	}
}

func generateDiagram(cmd *cobra.Command, o *options, rgdf *v1alpha1.ResourceGraphDefinition) error {
	rgdGraph, err := o.env.BuildGraph(rgdf, &o.gf, o.flags.Kubeconfig, o.flags.Context)
	if err != nil {
		return fmt.Errorf("failed to setup rgd graph: %w", err)
	}
	return renderDiagram(cmd, rgdf.GetName(), rgdGraph)
}

// renderDiagram renders a kro graph as an interactive HTML page.
func renderDiagram(cmd *cobra.Command, name string, rgdGraph *graph.Graph) error {
	graphChart := charts.NewGraph()

	// Graph layout
	graphChart.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Width:  "70%",
			Height: "90vh",
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    fmt.Sprintf("Resource Graph: %s", name),
			Subtitle: fmt.Sprintf("Topological Order: %v", rgdGraph.TopologicalOrder),
			TitleStyle: &opts.TextStyle{
				FontSize:   18,
				FontWeight: "bold",
			},
			SubtitleStyle: &opts.TextStyle{
				FontSize: 14,
			},
		}),
		charts.WithLegendOpts(opts.Legend{
			Show: opts.Bool(false),
		}),
	)

	resourceOrder := make(map[string]int)
	for i, resourceName := range rgdGraph.TopologicalOrder {
		resourceOrder[resourceName] = i + 1
	}

	names := make([]string, 0, len(rgdGraph.Resources))
	for resourceName := range rgdGraph.Resources {
		names = append(names, resourceName)
	}
	slices.Sort(names)

	// Graph nodes
	nodes := make([]opts.GraphNode, 0, len(rgdGraph.Resources))
	for _, resourceName := range names {
		order := resourceOrder[resourceName]

		node := opts.GraphNode{
			Name:       fmt.Sprintf("%s\n(%d)", resourceName, order),
			SymbolSize: 40.0,
			ItemStyle: &opts.ItemStyle{
				Color:       "#269103",
				BorderColor: "#333",
				BorderWidth: 1,
			},
		}
		nodes = append(nodes, node)
	}

	// Graph links
	links := make([]opts.GraphLink, 0)
	for _, resourceName := range names {
		resource := rgdGraph.Resources[resourceName]
		targetOrder := resourceOrder[resourceName]

		for _, dependency := range resource.Meta.Dependencies {
			sourceOrder := resourceOrder[dependency]

			link := opts.GraphLink{
				Source: fmt.Sprintf("%s\n(%d)", dependency, sourceOrder),
				Target: fmt.Sprintf("%s\n(%d)", resourceName, targetOrder),
				LineStyle: &opts.LineStyle{
					Color: "#000",
				},
			}
			links = append(links, link)
		}
	}

	graphChart.AddSeries("resource", nodes, links).SetSeriesOptions(
		charts.WithGraphChartOpts(
			opts.GraphChart{
				Force: &opts.GraphForce{
					Repulsion: 5000,
				},
				Roam:           opts.Bool(true),
				EdgeSymbol:     "arrow",
				EdgeSymbolSize: []int{0, 7},
			},
		),
	)

	return graphChart.Render(cmd.OutOrStdout())
}
