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

package validate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/instance"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

func newInstanceCommand(o *options) *cobra.Command {
	var rgdFile, instanceFile string

	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Validate instance manifests against the generated CRD",
		Long: "Validate instance manifests against the CRD generated from a " +
			"ResourceGraphDefinition, using the same defaulting, pruning, " +
			"OpenAPI and CEL checks the API server applies.\n\n" +
			"The instance file may contain multiple YAML documents.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runValidateInstance(cmd, o, rgdFile, instanceFile)
		},
	}
	cmd.Flags().StringVarP(&rgdFile, "rgd-file", "r", "",
		"Path to the ResourceGraphDefinition file (\"-\" for stdin; required)")
	cmd.Flags().StringVarP(&instanceFile, "instance-file", "i", "",
		"Path to the instance manifest, possibly multi-document (\"-\" for stdin; required)")
	clienv.RegisterGraphFlags(cmd, &o.gf)
	return cmd
}

func runValidateInstance(cmd *cobra.Command, o *options, rgdFile, instanceFile string) error {
	if rgdFile == "" || instanceFile == "" {
		return fmt.Errorf("both --rgd-file/-r and --instance-file/-i are required")
	}
	if rgdFile == "-" && instanceFile == "-" {
		return fmt.Errorf("only one of --rgd-file and --instance-file may be \"-\" (stdin)")
	}

	rgdf, err := rgdLoad(cmd, rgdFile)
	if err != nil {
		return err
	}

	g, err := o.env.BuildGraph(rgdf, &o.gf, o.flags.Kubeconfig, o.flags.Context)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	crd := g.CRD

	docs, err := readObjects(cmd, instanceFile)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("instance file %q contains no documents", instanceFile)
	}

	invalid := 0
	for _, doc := range docs {
		errs := validateInstanceDoc(cmd.Context(), crd, doc)
		kind := doc.GetKind()
		name := doc.GetName()
		if name == "" {
			name = doc.GetGenerateName() + "*"
		}
		if len(errs) == 0 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s/%s: valid\n", kind, name)
			continue
		}
		invalid++
		for _, e := range errs {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s/%s: %s\n", kind, name, e.Error())
		}
	}
	if invalid > 0 {
		return fmt.Errorf("%d instance document(s) invalid", invalid)
	}
	return nil
}

// rgdLoad loads an RGD file, "-" meaning stdin.
func rgdLoad(cmd *cobra.Command, path string) (*v1alpha1.ResourceGraphDefinition, error) {
	return rgd.LoadFile(path, cmd.InOrStdin())
}

// readObjects decodes a (possibly multi-document) YAML file into objects.
func readObjects(cmd *cobra.Command, path string) ([]*unstructured.Unstructured, error) {
	var r io.Reader
	if path == "-" {
		r = cmd.InOrStdin()
	} else {
		f, err := os.Open(path) // #nosec G304 -- user-supplied CLI argument
		if err != nil {
			return nil, fmt.Errorf("failed to read instance file: %w", err)
		}
		defer func() { _ = f.Close() }()
		r = f
	}

	var docs []*unstructured.Unstructured
	dec := utilyaml.NewYAMLReader(bufio.NewReader(r))
	for {
		doc, err := dec.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read instance file: %w", err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal(doc, &obj); err != nil {
			return nil, fmt.Errorf("failed to parse instance document: %w", err)
		}
		if len(obj) == 0 {
			continue
		}
		docs = append(docs, &unstructured.Unstructured{Object: obj})
	}
	return docs, nil
}

// validateInstanceDoc runs the metadata checks and schema validation for one
// instance document.
func validateInstanceDoc(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition, obj *unstructured.Unstructured) []error {
	var errs []error

	apiVersion := obj.GetAPIVersion()
	kind := obj.GetKind()
	gv, _ := schema.ParseGroupVersion(apiVersion)

	if gv.Group != crd.Spec.Group {
		errs = append(errs, fmt.Errorf("apiVersion group %q does not match CRD group %q", gv.Group, crd.Spec.Group))
	}
	if kind != crd.Spec.Names.Kind {
		errs = append(errs, fmt.Errorf("kind %q does not match CRD kind %q", kind, crd.Spec.Names.Kind))
	}

	var served *apiextensionsv1.CustomResourceDefinitionVersion
	for i := range crd.Spec.Versions {
		v := &crd.Spec.Versions[i]
		if v.Name == gv.Version && v.Served {
			served = v
		}
	}
	if gv.Group == crd.Spec.Group && served == nil {
		errs = append(errs, fmt.Errorf("apiVersion %q is not a served version of %s", apiVersion, crd.Spec.Names.Plural+"."+crd.Spec.Group))
	}

	if obj.GetName() == "" && obj.GetGenerateName() == "" {
		errs = append(errs, fmt.Errorf("metadata.name or metadata.generateName is required"))
	}

	clusterScoped := crd.Spec.Scope == apiextensionsv1.ClusterScoped
	if clusterScoped && obj.GetNamespace() != "" {
		errs = append(errs, fmt.Errorf("metadata.namespace is not allowed for cluster-scoped kind %q", kind))
	}

	if len(errs) > 0 || served == nil {
		return errs
	}

	s, err := instance.NewSchema(crd, served.Name)
	if err != nil {
		return append(errs, err)
	}
	for _, fe := range s.Validate(ctx, obj.Object) {
		errs = append(errs, fmt.Errorf("%s", fe.Error()))
	}
	return errs
}
