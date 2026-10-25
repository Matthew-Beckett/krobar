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

// Package cli constructs the krobar command tree.
package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/cli/fmtcmd"
	"github.com/Matthew-Beckett/krobar/internal/cli/generate"
	"github.com/Matthew-Beckett/krobar/internal/cli/render"
	"github.com/Matthew-Beckett/krobar/internal/cli/testcmd"
	"github.com/Matthew-Beckett/krobar/internal/cli/validate"
	"github.com/Matthew-Beckett/krobar/internal/version"
)

// NewRootCommand builds the root `krobar` command. An optional Env may be
// supplied to stub cluster access in tests; nil uses the real implementation.
func NewRootCommand(envs ...*clienv.Env) *cobra.Command {
	env := clienv.Default()
	if len(envs) > 0 && envs[0] != nil {
		env = envs[0]
	}
	flags := &clienv.Flags{}

	var verbose bool

	cmd := &cobra.Command{
		Use:   "krobar",
		Short: "krobar - interim Kube Resource Orchestrator CLI",
		Long: `krobar helps developers and administrators manage
ResourceGraphDefinitions (RGDs) and their instances in Kubernetes clusters.

It is an interim CLI re-implementing the kro CLI until an official one ships.`,
		Version:       version.Get().Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if env.Logger == nil {
				if verbose {
					env.Logger = slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelDebug}))
				} else {
					env.Logger = clienv.DiscardLogger()
				}
			}
		},
	}

	cmd.PersistentFlags().StringVar(&flags.Kubeconfig, "kubeconfig", "",
		"Path to kubeconfig file (default: standard loading rules: $KUBECONFIG or ~/.kube/config)")
	cmd.PersistentFlags().StringVar(&flags.Context, "context", "",
		"Kubernetes context to use")
	cmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")

	cmd.AddCommand(
		generate.NewCommand(env, flags),
		validate.NewCommand(env, flags),
		render.NewCommand(env, flags),
		testcmd.NewCommand(env, flags),
		fmtcmd.NewCommand(),
		newVersionCommand(),
	)
	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Get()
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "krobar %s (commit %s, built %s; bundled Kubernetes schemas %s)\n",
				info.Version, info.Commit, info.Date, info.Kubernetes)
			return err
		},
	}
}
