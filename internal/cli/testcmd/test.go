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

// Package testcmd implements `krobar test`: discover test suites and run
// their cases against the simulated reconciliation engine.
package testcmd

import (
	"fmt"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/exitcode"
	"github.com/Matthew-Beckett/krobar/internal/ktest/discover"
	"github.com/Matthew-Beckett/krobar/internal/ktest/report"
	"github.com/Matthew-Beckett/krobar/internal/ktest/run"
)

type options struct {
	env   *clienv.Env
	flags *clienv.Flags
	gf    clienv.GraphFlags
}

// NewCommand constructs the `test` command.
func NewCommand(env *clienv.Env, flags *clienv.Flags) *cobra.Command {
	o := &options{env: env, flags: flags}
	var runFilter, junitFile string
	var update bool

	cmd := &cobra.Command{
		Use:   "test [PATH...]",
		Short: "Run test suites",
		Long: "Discover test suites under the given paths (default \".\") and " +
			"run their cases. A suite is a directory containing a " +
			"krobar-test.yaml manifest (kind TestSuite), or a directory " +
			"with a tests/ subdirectory next to a single RGD file. Each " +
			"case builds the suite's graph once, simulates reconciliation " +
			"with its instance and observed objects, and checks the " +
			"declared expectations.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCmd(cmd, o, args, runFilter, junitFile, update)
		},
	}
	cmd.Flags().StringVar(&runFilter, "run", "", "Only run tests whose path (suite/case) matches this regular expression")
	cmd.Flags().StringVar(&junitFile, "junit", "", "Write a JUnit XML report to this file")
	cmd.Flags().BoolVarP(&update, "update-snapshots", "u", false, "Write/update case snapshot files (expected/) instead of comparing them")
	clienv.RegisterGraphFlags(cmd, &o.gf)
	return cmd
}

func runCmd(cmd *cobra.Command, o *options, args []string, runFilter, junitFile string, update bool) error {
	if len(args) == 0 {
		args = []string{"."}
	}
	plan, err := discover.Discover(args)
	if err != nil {
		return err
	}
	if countCases(plan) == 0 {
		return exitcode.New(exitcode.Failed,
			fmt.Errorf("no tests found under %v", args))
	}

	var filter run.Filter
	if runFilter != "" {
		re, err := regexp.Compile(runFilter)
		if err != nil {
			return fmt.Errorf("invalid --run regex: %w", err)
		}
		filter = re.MatchString
	}

	text := &report.Text{W: cmd.OutOrStdout()}
	junit := &report.JUnit{}
	runner := &run.Runner{
		Env:        o.env,
		GraphFlags: &o.gf,
		Kubeconfig: o.flags.Kubeconfig,
		Context:    o.flags.Context,
		Sinks:      []run.Sink{text, junit},
		Filter:     filter,
		Update:     update,
	}
	summary := runner.Run(plan)

	if filter != nil && summary.Total() == 0 {
		return exitcode.New(exitcode.Failed,
			fmt.Errorf("no tests matched --run %q", runFilter))
	}
	text.Finish(summary)

	if junitFile != "" {
		if err := junit.Write(junitFile); err != nil {
			return err
		}
	}
	if summary.Failed > 0 {
		return exitcode.New(exitcode.Failed, fmt.Errorf("%d test(s) failed", summary.Failed))
	}
	return nil
}

// countCases counts runnable cases plus suite-level errors.
func countCases(p *discover.Plan) int {
	n := 0
	for _, s := range p.Suites {
		if s.Err != nil {
			n++
			continue
		}
		n += len(s.Cases)
	}
	return n
}
