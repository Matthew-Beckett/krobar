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

// Package render implements `krobar render`: it simulates one kro
// reconciliation offline and prints the resources the controller would
// create, in topological order.
package render

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/objects"
	"github.com/Matthew-Beckett/krobar/internal/output"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
	"github.com/Matthew-Beckett/krobar/internal/simulate"
)

type options struct {
	env   *clienv.Env
	flags *clienv.Flags
	gf    clienv.GraphFlags
}

// NewCommand constructs the `render` command.
func NewCommand(env *clienv.Env, flags *clienv.Flags) *cobra.Command {
	o := &options{env: env, flags: flags}
	var rgdFile, instanceFile, format string
	var observedPaths []string
	var includeInstance bool

	cmd := &cobra.Command{
		Use:   "render -f RGD -i INSTANCE",
		Short: "Render the resources an instance would create, offline",
		Long: "Render simulates one kro reconciliation locally and prints the " +
			"resources the controller would create, in topological order.\n\n" +
			"Observed objects (what already exists in the cluster) can be " +
			"supplied with --observed; they are merged on top of the desired " +
			"objects and drive readyWhen evaluation and status projection. " +
			"External references need an observed object to resolve.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, o, rgdFile, instanceFile, observedPaths, includeInstance, format)
		},
	}
	cmd.Flags().StringVarP(&rgdFile, "file", "f", "",
		"Path to the ResourceGraphDefinition file (\"-\" for stdin; required)")
	cmd.Flags().StringVarP(&instanceFile, "instance-file", "i", "",
		"Path to the instance manifest, exactly one document (\"-\" for stdin; required)")
	cmd.Flags().StringArrayVar(&observedPaths, "observed", nil,
		"Files or directories of already-existing objects (repeatable; multi-doc YAML and List documents accepted)")
	cmd.Flags().BoolVar(&includeInstance, "include-instance", false,
		"Append the instance (with computed status) to the output")
	cmd.Flags().StringVarP(&format, "output", "o", "yaml",
		"Output format: yaml (document stream) or json (v1 List)")
	clienv.RegisterGraphFlags(cmd, &o.gf)
	return cmd
}

func run(cmd *cobra.Command, o *options, rgdFile, instanceFile string, observedPaths []string, includeInstance bool, format string) error {
	if rgdFile == "" || instanceFile == "" {
		return fmt.Errorf("both --file/-f and --instance-file/-i are required")
	}
	if rgdFile == "-" && instanceFile == "-" {
		return fmt.Errorf("only one of --file and --instance-file may be \"-\" (stdin)")
	}
	f, err := output.ParseFormat(format)
	if err != nil {
		return err
	}

	o.env.Log("loading ResourceGraphDefinition file", "file", rgdFile)
	rgdf, err := rgd.LoadFile(rgdFile, cmd.InOrStdin())
	if err != nil {
		return err
	}
	g, err := o.env.BuildGraph(rgdf, &o.gf, o.flags.Kubeconfig, o.flags.Context)
	if err != nil {
		return err
	}

	instances, err := objects.ReadFile(instanceFile, cmd.InOrStdin())
	if err != nil {
		return err
	}
	if len(instances) != 1 {
		return fmt.Errorf("instance file must contain exactly one document, got %d", len(instances))
	}

	var observed []*unstructured.Unstructured
	for _, p := range observedPaths {
		objs, err := objects.ReadPath(p)
		if err != nil {
			return err
		}
		observed = append(observed, objs...)
	}

	res, err := simulate.Run(g, instances[0], observed)
	if err != nil {
		return err
	}

	// stdout: rendered objects in topological order.
	var rendered []*unstructured.Unstructured
	for _, n := range res.Nodes {
		rendered = append(rendered, n.Objects...)
	}
	if includeInstance {
		rendered = append(rendered, res.Instance)
	}
	if err := writeObjects(cmd, rendered, f); err != nil {
		return err
	}

	// stderr: summary table + unmatched-observed warnings.
	w := tabwriter.NewWriter(cmd.ErrOrStderr(), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NODE\tSTATE\tDETAIL")
	for _, n := range res.Nodes {
		detail := n.Reason
		if detail == "" {
			detail = fmt.Sprintf("%d object(s)", len(n.Objects))
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", n.ID, n.State, detail)
	}
	_ = w.Flush()
	for _, u := range res.UnmatchedObserved {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: observed object %s %s/%s did not match any node\n",
			u.GroupVersionKind().String(), u.GetNamespace(), u.GetName())
	}
	return nil
}

func writeObjects(cmd *cobra.Command, objs []*unstructured.Unstructured, f output.Format) error {
	out := cmd.OutOrStdout()
	switch f {
	case output.FormatYAML:
		for i, obj := range objs {
			if i > 0 {
				if _, err := fmt.Fprintln(out, "---"); err != nil {
					return err
				}
			}
			if err := output.Write(out, obj.Object, f); err != nil {
				return err
			}
		}
		return nil
	case output.FormatJSON:
		list := map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "List",
			"items":      []interface{}{},
		}
		for _, obj := range objs {
			list["items"] = append(list["items"].([]interface{}), obj.Object)
		}
		return output.Write(out, list, f)
	default:
		return fmt.Errorf("unsupported format %q", f)
	}
}
