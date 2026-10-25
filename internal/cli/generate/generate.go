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

// Package generate implements the `krobar generate` command family.
package generate

import (
	"github.com/spf13/cobra"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/output"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

type options struct {
	env    *clienv.Env
	flags  *clienv.Flags
	gf     clienv.GraphFlags
	file   string
	format string
}

// NewCommand constructs the `generate` command and its subcommands.
func NewCommand(env *clienv.Env, flags *clienv.Flags) *cobra.Command {
	o := &options{env: env, flags: flags}

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate artefacts from a ResourceGraphDefinition",
		Long: "Generate artefacts from a ResourceGraphDefinition.\n\n" +
			"The file is supplied via -f/--file or a single positional argument; " +
			"\"-\" reads from standard input.",
	}
	cmd.PersistentFlags().StringVarP(&o.file, "file", "f", "",
		"Path to the ResourceGraphDefinition file (\"-\" for stdin)")
	cmd.PersistentFlags().StringVarP(&o.format, "format", "o", string(output.FormatYAML),
		"Output format (yaml|json)")
	clienv.RegisterGraphFlags(cmd, &o.gf)

	cmd.AddCommand(newCRDCommand(o), newInstanceCommand(o), newDiagramCommand(o))
	return cmd
}

// parseFormat validates the -o flag before any cluster access happens.
func (o *options) parseFormat() (output.Format, error) {
	return output.ParseFormat(o.format)
}

// load resolves the file selection (-f or positional) and parses the RGD.
func (o *options) load(cmd *cobra.Command, args []string) (*v1alpha1.ResourceGraphDefinition, error) {
	path, err := clienv.ResolveFile(o.file, args)
	if err != nil {
		return nil, err
	}
	o.env.Log("loading ResourceGraphDefinition file", "file", path)
	return rgd.LoadFile(path, cmd.InOrStdin())
}
