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
	"fmt"

	"github.com/spf13/cobra"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/kubernetes-sigs/kro/pkg/graph/crd/compat"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/exitcode"
	"github.com/Matthew-Beckett/krobar/internal/output"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

func newCompatCommand(o *options) *cobra.Command {
	var oldFile, newFile, format string

	cmd := &cobra.Command{
		Use:   "compat OLD_RGD NEW_RGD",
		Short: "Check whether a new RGD breaks compatibility with an old one",
		Long: "Compare the CRDs generated from two ResourceGraphDefinitions and " +
			"report breaking and non-breaking schema changes, like the kro " +
			"controller does when rejecting incompatible RGD updates.\n\n" +
			"Exits 0 when compatible, 1 when breaking changes are found.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompat(cmd, o, oldFile, newFile, format, args)
		},
	}
	cmd.Flags().StringVar(&oldFile, "old", "", "Path to the old ResourceGraphDefinition file")
	cmd.Flags().StringVar(&newFile, "new", "", "Path to the new ResourceGraphDefinition file")
	cmd.Flags().StringVarP(&format, "format", "o", "",
		"Output format for the report (yaml|json; default human-readable)")
	clienv.RegisterGraphFlags(cmd, &o.gf)
	return cmd
}

func runCompat(cmd *cobra.Command, o *options, oldFile, newFile, format string, args []string) error {
	// Resolve file selection: two positionals or --old/--new.
	if len(args) == 2 {
		if oldFile != "" || newFile != "" {
			return fmt.Errorf("files provided both positionally and via --old/--new; use only one")
		}
		oldFile, newFile = args[0], args[1]
	} else if len(args) != 0 {
		return fmt.Errorf("expected zero or two positional arguments (OLD_RGD NEW_RGD)")
	}
	if oldFile == "" || newFile == "" {
		return fmt.Errorf("old and new ResourceGraphDefinition files are required (positionally or via --old/--new)")
	}

	var of output.Format
	if format != "" {
		var err error
		of, err = output.ParseFormat(format)
		if err != nil {
			return err
		}
	}

	build := func(path string) (*apiextensionsv1.CustomResourceDefinition, error) {
		r, err := rgd.LoadFile(path, cmd.InOrStdin())
		if err != nil {
			return nil, err
		}
		g, err := o.env.BuildGraph(r, &o.gf, o.flags.Kubeconfig, o.flags.Context)
		if err != nil {
			return nil, err
		}
		return g.CRD, nil
	}

	oldCRD, err := build(oldFile)
	if err != nil {
		return fmt.Errorf("failed to build old ResourceGraphDefinition: %w", err)
	}
	newCRD, err := build(newFile)
	if err != nil {
		return fmt.Errorf("failed to build new ResourceGraphDefinition: %w", err)
	}

	if oldCRD.Spec.Group != newCRD.Spec.Group || oldCRD.Spec.Names.Kind != newCRD.Spec.Names.Kind {
		return fmt.Errorf("not the same API: %s/%s vs %s/%s",
			oldCRD.Spec.Group, oldCRD.Spec.Names.Kind, newCRD.Spec.Group, newCRD.Spec.Names.Kind)
	}

	report, err := compat.CompareVersions(oldCRD.Spec.Versions, newCRD.Spec.Versions)
	if err != nil {
		return fmt.Errorf("failed to compare versions: %w", err)
	}

	if format != "" {
		if err := output.Write(cmd.OutOrStdout(), report, of); err != nil {
			return err
		}
	} else {
		printReport(cmd, report)
	}

	if report.HasBreakingChanges() {
		return exitcode.New(exitcode.Failed, fmt.Errorf("breaking changes detected"))
	}
	return nil
}

func printReport(cmd *cobra.Command, report *compat.Report) {
	out := cmd.OutOrStdout()
	if len(report.BreakingChanges) > 0 {
		_, _ = fmt.Fprintln(out, "Breaking changes:")
		for _, c := range report.BreakingChanges {
			_, _ = fmt.Fprintf(out, "  - %s\n", c.Description())
		}
	}
	if len(report.NonBreakingChanges) > 0 {
		_, _ = fmt.Fprintln(out, "Non-breaking changes:")
		for _, c := range report.NonBreakingChanges {
			_, _ = fmt.Fprintf(out, "  - %s\n", c.Description())
		}
	}
	if !report.HasChanges() {
		_, _ = fmt.Fprintln(out, "No changes detected; schemas are compatible.")
	} else if !report.HasBreakingChanges() {
		_, _ = fmt.Fprintln(out, "Compatible: no breaking changes.")
	}
}
