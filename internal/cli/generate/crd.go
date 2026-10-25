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

	"github.com/Matthew-Beckett/krobar/internal/output"
	"github.com/Matthew-Beckett/krobar/internal/version"
)

func newCRDCommand(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "crd [FILE]",
		Short: "Generate a CustomResourceDefinition (CRD)",
		Long: "Generate a CustomResourceDefinition (CRD) from a " +
			"ResourceGraphDefinition file. This command reads the " +
			"ResourceGraphDefinition and outputs the corresponding CRD " +
			"in the specified format.",
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
				return fmt.Errorf("failed to setup rgd graph: %w", err)
			}

			crd := g.CRD
			// The builder leaves TypeMeta empty; fill it so output is
			// kubectl-applyable.
			if crd.APIVersion == "" {
				crd.APIVersion = extv1.SchemeGroupVersion.String()
			}
			if crd.Kind == "" {
				crd.Kind = "CustomResourceDefinition"
			}
			annotations := crd.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations["kro.run/cli-version"] = version.Get().Version
			crd.SetAnnotations(annotations)

			if err := output.Write(cmd.OutOrStdout(), crd, format); err != nil {
				return fmt.Errorf("failed to marshal CRD: %w", err)
			}
			return nil
		},
	}
}
