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

// Package validate implements the `krobar validate` command family.
package validate

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

type options struct {
	env   *clienv.Env
	flags *clienv.Flags
	gf    clienv.GraphFlags
}

// NewCommand constructs the `validate` command and its subcommands.
func NewCommand(env *clienv.Env, flags *clienv.Flags) *cobra.Command {
	o := &options{env: env, flags: flags}

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate ResourceGraphDefinitions and instances",
		Long: "Validate ResourceGraphDefinitions and their instances.\n\n" +
			"Validation builds the kro resource graph offline by default " +
			"(compiled-in Kubernetes schemas plus CRDs supplied via --crds); " +
			"use --from-cluster to resolve schemas from a live cluster.",
	}

	cmd.AddCommand(newRGDCommand(o), newInstanceCommand(o), newCompatCommand(o))
	return cmd
}

func newRGDCommand(o *options) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "rgd [FILE]",
		Short: "Validate a ResourceGraphDefinition file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := clienv.ResolveFile(file, args)
			if err != nil {
				return err
			}
			o.env.Log("loading ResourceGraphDefinition file", "file", path)
			rgdf, err := rgd.LoadFile(path, cmd.InOrStdin())
			if err != nil {
				return err
			}

			if _, err := o.env.BuildGraph(rgdf, &o.gf, o.flags.Kubeconfig, o.flags.Context); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Validation successful! The ResourceGraphDefinition is valid.")
			return err
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "",
		"Path to the ResourceGraphDefinition file (\"-\" for stdin)")
	clienv.RegisterGraphFlags(cmd, &o.gf)
	return cmd
}
