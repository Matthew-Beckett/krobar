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

	"github.com/spf13/cobra"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sschema "k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubernetes-sigs/kro/pkg/graph/schema"

	"github.com/Matthew-Beckett/krobar/internal/emulator"
	"github.com/Matthew-Beckett/krobar/internal/output"
	"github.com/Matthew-Beckett/krobar/internal/version"
)

func newInstanceCommand(o *options) *cobra.Command {
	var seed int64
	var namespace string

	cmd := &cobra.Command{
		Use:   "instance [FILE]",
		Short: "Generate an instance of the ResourceGraphDefinition",
		Long: "Generate a ResourceGraphDefinition (RGD) instance from a " +
			"ResourceGraphDefinition file. This command reads the " +
			"ResourceGraphDefinition and outputs a dummy instance of the " +
			"generated CRD.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := o.parseFormat()
			if err != nil {
				return err
			}
			rgdf, err := o.load(cmd, args)
			if err != nil {
				return err
			}

			g, err := o.env.BuildGraph(rgdf, &o.gf, o.flags.Kubeconfig, o.flags.Context)
			if err != nil {
				return fmt.Errorf("failed to create resource graph definition: %w", err)
			}

			var em *emulator.Emulator
			if seed != 0 {
				em = emulator.NewEmulatorWithSeed(seed)
			} else {
				em = emulator.NewEmulator()
			}

			crd := g.CRD
			if len(crd.Spec.Versions) == 0 {
				return fmt.Errorf("generated CRD has no versions")
			}
			// Use the storage version (falling back to the first) for both the
			// instance apiVersion and the OpenAPI schema.
			crdVersion := &crd.Spec.Versions[0]
			for i := range crd.Spec.Versions {
				if crd.Spec.Versions[i].Storage {
					crdVersion = &crd.Spec.Versions[i]
					break
				}
			}
			if crdVersion.Schema == nil || crdVersion.Schema.OpenAPIV3Schema == nil {
				return fmt.Errorf("generated CRD has no OpenAPI v3 schema")
			}

			// The instance GVK comes from the CRD spec, not the CRD object's
			// own TypeMeta (which is apiextensions/v1 CustomResourceDefinition).
			gvk := k8sschema.GroupVersionKind{
				Group:   crd.Spec.Group,
				Version: crdVersion.Name,
				Kind:    crd.Spec.Names.Kind,
			}

			s, err := schema.ConvertJSONSchemaPropsToSpecSchema(crdVersion.Schema.OpenAPIV3Schema)
			if err != nil {
				return fmt.Errorf("failed to convert JSONSchemaProps to SpecSchema: %w", err)
			}

			emulatedObj, err := em.GenerateDummyCR(gvk, s)
			if err != nil {
				return fmt.Errorf("failed to generate dummy CR: %w", err)
			}
			if crd.Spec.Scope == extv1.ClusterScoped {
				emulatedObj.SetNamespace("") // namespace ignored for cluster-scoped kinds
			} else {
				emulatedObj.SetNamespace(namespace)
			}

			annotations := emulatedObj.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations["kro.run/version"] = version.Get().Version
			emulatedObj.SetAnnotations(annotations)

			delete(emulatedObj.Object, "status")

			if err := output.Write(cmd.OutOrStdout(), emulatedObj, format); err != nil {
				return fmt.Errorf("failed to marshal instance: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().Int64Var(&seed, "seed", 0, "Seed for deterministic dummy data generation (0 = time-based)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Namespace for the generated instance (ignored for cluster-scoped kinds)")
	return cmd
}
