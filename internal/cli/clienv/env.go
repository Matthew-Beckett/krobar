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

// Package clienv holds the shared dependencies injected into CLI commands so
// tests can stub out cluster access.
package clienv

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/graph"

	"github.com/Matthew-Beckett/krobar/internal/kube"
	"github.com/Matthew-Beckett/krobar/internal/offline"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

// Flags holds the values of the persistent root flags, shared by pointer
// with subcommands so they are populated after flag parsing.
type Flags struct {
	Kubeconfig string
	Context    string
}

// GraphFlags holds the graph-source flags registered on every graph-building
// command (--crds / --from-cluster).
type GraphFlags struct {
	CRDs        []string
	FromCluster bool
}

// RegisterGraphFlags adds --crds and --from-cluster to cmd.
func RegisterGraphFlags(cmd *cobra.Command, gf *GraphFlags) {
	cmd.PersistentFlags().StringArrayVar(&gf.CRDs, "crds", nil,
		"Files or directories with CRDs used for offline schema resolution (repeatable; may contain RGDs to compile)")
	cmd.PersistentFlags().BoolVar(&gf.FromCluster, "from-cluster", false,
		"Resolve schemas via the live cluster instead of offline (requires --kubeconfig/--context)")
}

// Env carries the injectable seams shared by all commands.
type Env struct {
	// ResolveRESTConfig turns --kubeconfig/--context flag values into a
	// *rest.Config. Defaults to kube.RESTConfig.
	ResolveRESTConfig func(kubeconfig, context string) (*rest.Config, error)

	// NewGraphBuilder returns a GraphBuilder for the given rest config.
	// Defaults to rgd.NewClusterGraphBuilder.
	NewGraphBuilder func(cfg *rest.Config) rgd.GraphBuilder

	// NewOfflineGraphBuilder loads the CRDs at the given paths and returns an
	// offline GraphBuilder. Defaults to a real implementation.
	NewOfflineGraphBuilder func(crdPaths []string) (rgd.GraphBuilder, error)

	// Logger receives diagnostic output. May be nil (discarded).
	Logger *slog.Logger
}

// Default returns an Env wired to the real cluster implementations.
func Default() *Env {
	return &Env{
		ResolveRESTConfig:      kube.RESTConfig,
		NewGraphBuilder:        rgd.NewClusterGraphBuilder,
		NewOfflineGraphBuilder: defaultOfflineBuilder,
	}
}

func defaultOfflineBuilder(crdPaths []string) (rgd.GraphBuilder, error) {
	crds, rgds, err := offline.LoadDocs(crdPaths)
	if err != nil {
		return nil, err
	}

	// Compile RGDs found in --crds into their generated CRDs, multi-pass so
	// chained RGDs that reference each other's instance kinds work. Report
	// the last error for any that never compile.
	remaining := rgds
	var lastErr error
	for len(remaining) > 0 {
		set, err := offline.NewSet(crds)
		if err != nil {
			return nil, err
		}
		b := rgd.NewOfflineGraphBuilder(set)
		progress := false
		var next []offline.RGDDoc
		for _, doc := range remaining {
			rgd.ApplyDefaults(doc.RGD)
			g, err := b.Build(doc.RGD)
			if err != nil {
				lastErr = fmt.Errorf("%s: %w", doc.Source, err)
				next = append(next, doc)
				continue
			}
			crds = append(crds, g.CRD)
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("failed to compile ResourceGraphDefinition(s) from --crds: %w", lastErr)
		}
		remaining = next
	}

	set, err := offline.NewSet(crds)
	if err != nil {
		return nil, err
	}
	return rgd.NewOfflineGraphBuilder(set), nil
}

// Log logs at Debug level if a logger is configured.
func (e *Env) Log(msg string, args ...any) {
	if e.Logger != nil {
		e.Logger.Debug(msg, args...)
	}
}

// BuildGraph builds the graph for the RGD, offline by default or via the
// cluster when gf.FromCluster is set.
func (e *Env) BuildGraph(r *v1alpha1.ResourceGraphDefinition, gf *GraphFlags, kubeconfig, context string) (*graph.Graph, error) {
	if gf != nil && gf.FromCluster {
		e.Log("resolving kubeconfig/context", "kubeconfig", kubeconfig, "context", context)
		cfg, err := e.ResolveRESTConfig(kubeconfig, context)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve Kubernetes configuration: %w", err)
		}
		e.Log("building graph via cluster")
		return e.NewGraphBuilder(cfg).Build(r)
	}
	crdPaths := []string(nil)
	if gf != nil {
		crdPaths = gf.CRDs
	}
	e.Log("building graph offline", "crds", crdPaths)
	builder, err := e.NewOfflineGraphBuilder(crdPaths)
	if err != nil {
		return nil, err
	}
	return builder.Build(r)
}

// ResolveFile merges the -f/--file flag value with at most one positional
// argument. Exactly one of the two must be provided; "-" selects stdin.
func ResolveFile(flagValue string, args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("expected at most one ResourceGraphDefinition file argument, got %d", len(args))
	}
	positional := ""
	if len(args) == 1 {
		positional = args[0]
	}
	switch {
	case flagValue != "" && positional != "":
		return "", fmt.Errorf("ResourceGraphDefinition file provided both via --file and positional argument; use only one")
	case flagValue == "" && positional == "":
		return "", fmt.Errorf("ResourceGraphDefinition file is required")
	case flagValue != "":
		return flagValue, nil
	default:
		return positional, nil
	}
}

// DiscardLogger returns a logger that drops everything.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
