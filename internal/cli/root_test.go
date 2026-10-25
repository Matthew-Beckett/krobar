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

package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/graph"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

const testRGDPath = "../../testdata/webapp-rgd.yaml"

type stubBuilder struct {
	graph *graph.Graph
	err   error
}

func (s stubBuilder) Build(_ *v1alpha1.ResourceGraphDefinition) (*graph.Graph, error) {
	return s.graph, s.err
}

func testGraph() *graph.Graph {
	return &graph.Graph{
		CRD: &extv1.CustomResourceDefinition{
			// TypeMeta intentionally empty, mirroring the real builder.
			ObjectMeta: metav1.ObjectMeta{Name: "webapps.kro.run"},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "kro.run",
				Names: extv1.CustomResourceDefinitionNames{
					Kind:     "WebApp",
					ListKind: "WebAppList",
					Plural:   "webapps",
					Singular: "webapp",
				},
				Scope: extv1.NamespaceScoped,
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:    "v1alpha1",
						Served:  true,
						Storage: true,
						Schema: &extv1.CustomResourceValidation{
							OpenAPIV3Schema: &extv1.JSONSchemaProps{
								Type: "object",
								Properties: map[string]extv1.JSONSchemaProps{
									"spec": {
										Type: "object",
										Properties: map[string]extv1.JSONSchemaProps{
											"name": {Type: "string"},
											"port": {Type: "integer"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		Resources: map[string]*graph.Node{
			"deployment": {},
			"service":    {Meta: graph.NodeMeta{Dependencies: []string{"deployment"}}},
		},
		Nodes:            map[string]*graph.Node{},
		TopologicalOrder: []string{"deployment", "service"},
	}
}

func testEnv(g *graph.Graph, buildErr error) *clienv.Env {
	return &clienv.Env{
		ResolveRESTConfig: func(_, _ string) (*rest.Config, error) {
			return &rest.Config{}, nil
		},
		NewGraphBuilder: func(*rest.Config) rgd.GraphBuilder {
			return stubBuilder{graph: g, err: buildErr}
		},
		NewOfflineGraphBuilder: func(_ []string) (rgd.GraphBuilder, error) {
			return stubBuilder{graph: g, err: buildErr}, nil
		},
	}
}

func run(t *testing.T, env *clienv.Env, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRootCommand(env)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String() + errBuf.String(), err
}

func readTestRGD(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(testRGDPath)
	require.NoError(t, err)
	return string(data)
}

func TestGenerateCRD(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		stdin     string
		wantErr   string
		wantInOut []string
	}{
		{
			name:      "yaml via -f",
			args:      []string{"generate", "crd", "-f", testRGDPath},
			wantInOut: []string{"apiVersion: apiextensions.k8s.io/v1", "kind: CustomResourceDefinition", "kro.run/cli-version"},
		},
		{
			name:      "json via positional",
			args:      []string{"generate", "crd", "-o", "json", testRGDPath},
			wantInOut: []string{`"apiVersion": "apiextensions.k8s.io/v1"`, `"kind": "CustomResourceDefinition"`, "kro.run/cli-version"},
		},
		{
			name:      "stdin",
			args:      []string{"generate", "crd", "-f", "-"},
			stdin:     readTestRGD(t),
			wantInOut: []string{"webapps.kro.run"},
		},
		{
			name:    "missing file",
			args:    []string{"generate", "crd"},
			wantErr: "ResourceGraphDefinition file is required",
		},
		{
			name:    "both -f and positional",
			args:    []string{"generate", "crd", "-f", testRGDPath, testRGDPath},
			wantErr: "use only one",
		},
		{
			name:    "bad format rejected before cluster access",
			args:    []string{"generate", "crd", "-o", "xml", "-f", testRGDPath},
			wantErr: "unsupported output format",
		},
		{
			name:    "nonexistent file",
			args:    []string{"generate", "crd", "-f", "nope.yaml"},
			wantErr: "failed to read",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := run(t, testEnv(testGraph(), nil), tt.stdin, tt.args...)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			for _, want := range tt.wantInOut {
				assert.Contains(t, out, want)
			}
		})
	}
}

func TestGenerateInstance(t *testing.T) {
	args := []string{"generate", "instance", "--seed", "7", "-f", testRGDPath}
	out1, err := run(t, testEnv(testGraph(), nil), "", args...)
	require.NoError(t, err)
	out2, err := run(t, testEnv(testGraph(), nil), "", args...)
	require.NoError(t, err)
	assert.Equal(t, out1, out2, "fixed seed should produce deterministic output")
	assert.Contains(t, out1, "apiVersion: kro.run/v1alpha1")
	assert.Contains(t, out1, "kind: WebApp")
	assert.Contains(t, out1, "namespace: default")
	assert.Contains(t, out1, "kro.run/version")
	assert.NotContains(t, out1, "status:")
}

func TestGenerateInstanceClusterScoped(t *testing.T) {
	g := testGraph()
	g.CRD.Spec.Scope = extv1.ClusterScoped
	out, err := run(t, testEnv(g, nil), "", "generate", "instance", "--seed", "7", "-f", testRGDPath)
	require.NoError(t, err)
	assert.Contains(t, out, "apiVersion: kro.run/v1alpha1")
	assert.Contains(t, out, "kind: WebApp")
	assert.NotContains(t, out, "namespace:")
}

func TestGenerateInstanceNoSchema(t *testing.T) {
	g := testGraph()
	g.CRD.Spec.Versions = nil
	_, err := run(t, testEnv(g, nil), "", "generate", "instance", "-f", testRGDPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no versions")
}

func TestGenerateDiagram(t *testing.T) {
	out, err := run(t, testEnv(testGraph(), nil), "", "generate", "diagram", "-f", testRGDPath)
	require.NoError(t, err)
	assert.Contains(t, out, "deployment")
	assert.Contains(t, out, "service")
}

func TestValidateRGD(t *testing.T) {
	out, err := run(t, testEnv(testGraph(), nil), "", "validate", "rgd", "-f", testRGDPath)
	require.NoError(t, err)
	assert.Contains(t, out, "Validation successful! The ResourceGraphDefinition is valid.")
}

func TestValidateRGDErrorWrapping(t *testing.T) {
	_, err := run(t, testEnv(nil, errors.New("boom")), "", "validate", "rgd", "-f", testRGDPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed: boom")
}

func TestValidateRGDNoFile(t *testing.T) {
	_, err := run(t, testEnv(testGraph(), nil), "", "validate", "rgd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResourceGraphDefinition file is required")
}

func TestVersion(t *testing.T) {
	out, err := run(t, testEnv(testGraph(), nil), "", "version")
	require.NoError(t, err)
	assert.Contains(t, out, "krobar")
}
