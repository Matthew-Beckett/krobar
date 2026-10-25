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

// Package testcmd implements `krobar test`: run declarative RGDTest
// fixtures against the simulated reconciliation engine.
package testcmd

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/spf13/cobra"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/exitcode"
	"github.com/Matthew-Beckett/krobar/internal/rgdtest"
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

	cmd := &cobra.Command{
		Use:   "test [PATH...]",
		Short: "Run RGDTest fixture files",
		Long: "Run declarative RGDTest fixtures (apiVersion krobar.dev/v1alpha1, " +
			"kind RGDTest) found in YAML files under the given paths " +
			"(default \".\"). Each test builds the referenced RGD's graph, " +
			"simulates reconciliation with the supplied observed objects, and " +
			"checks the declared expectations.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, o, args, runFilter, junitFile)
		},
	}
	cmd.Flags().StringVar(&runFilter, "run", "", "Only run tests whose name matches this regular expression")
	cmd.Flags().StringVar(&junitFile, "junit", "", "Write a JUnit XML report to this file")
	clienv.RegisterGraphFlags(cmd, &o.gf)
	return cmd
}

type foundTest struct {
	tc   *rgdtest.RGDTest
	file string
}

func run(cmd *cobra.Command, o *options, args []string, runFilter, junitFile string) error {
	if len(args) == 0 {
		args = []string{"."}
	}
	files, err := scanFiles(args)
	if err != nil {
		return err
	}

	var tests []foundTest
	for _, f := range files {
		fmts, err := loadTests(f)
		if err != nil {
			return err
		}
		tests = append(tests, fmts...)
	}
	if len(tests) == 0 {
		return exitcode.New(exitcode.Failed,
			fmt.Errorf("no RGDTest documents found under %v", args))
	}

	var re *regexp.Regexp
	if runFilter != "" {
		re, err = regexp.Compile(runFilter)
		if err != nil {
			return fmt.Errorf("invalid --run regex: %w", err)
		}
	}

	runner := &rgdtest.Runner{
		Env:        o.env,
		GraphFlags: &o.gf,
		Kubeconfig: o.flags.Kubeconfig,
		Context:    o.flags.Context,
	}

	var results []outcome
	passed, failed := 0, 0
	for _, ft := range tests {
		if re != nil && !re.MatchString(ft.tc.Metadata.Name) {
			continue
		}
		msgs := runner.Run(ft.tc, filepath.Dir(ft.file))
		if len(msgs) == 0 {
			passed++
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "--- PASS: %s (%s)\n", ft.tc.Metadata.Name, ft.file)
		} else {
			failed++
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "--- FAIL: %s (%s)\n", ft.tc.Metadata.Name, ft.file)
			for _, m := range msgs {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", m)
			}
		}
		results = append(results, outcome{ft, msgs})
	}
	if re != nil && len(results) == 0 {
		return exitcode.New(exitcode.Failed,
			fmt.Errorf("no tests matched --run %q", runFilter))
	}
	if failed == 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ok\t%d passed\n", passed)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "FAIL\t%d of %d failed\n", failed, passed+failed)
	}

	if junitFile != "" {
		if err := writeJUnit(junitFile, results); err != nil {
			return err
		}
	}
	if failed > 0 {
		return exitcode.New(exitcode.Failed, fmt.Errorf("%d test(s) failed", failed))
	}
	return nil
}

// scanFiles expands paths into a sorted list of YAML files.
func scanFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			switch filepath.Ext(fp) {
			case ".yaml", ".yml":
				files = append(files, fp)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

// loadTests returns the RGDTest documents in a file, in document order.
func loadTests(path string) ([]foundTest, error) {
	f, err := os.Open(path) // #nosec G304 -- user-supplied CLI argument
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []foundTest
	dec := utilyaml.NewYAMLReader(bufio.NewReader(f))
	for {
		doc, err := dec.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		tc, err := rgdtest.ParseDoc(doc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if tc != nil {
			out = append(out, foundTest{tc: tc, file: path})
		}
	}
}

// JUnit report structures.
type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}
type junitTestcase struct {
	Name    string        `xml:"name,attr"`
	File    string        `xml:"file,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
}
type junitTestsuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Testcases []junitTestcase `xml:"testcase"`
}

type outcome struct {
	ft       foundTest
	failures []string
}

func writeJUnit(path string, results []outcome) error {
	byFile := map[string][]outcome{}
	var order []string
	for _, r := range results {
		f := r.ft.file
		if _, ok := byFile[f]; !ok {
			order = append(order, f)
		}
		byFile[f] = append(byFile[f], r)
	}
	sort.Strings(order)

	var suites []junitTestsuite
	for _, f := range order {
		s := junitTestsuite{Name: f}
		for _, r := range byFile[f] {
			s.Tests++
			tc := junitTestcase{Name: r.ft.tc.Metadata.Name, File: f}
			if len(r.failures) > 0 {
				s.Failures++
				var text string
				for _, m := range r.failures {
					text += m + "\n"
				}
				tc.Failure = &junitFailure{Message: "expectation failed", Text: text}
			}
			s.Testcases = append(s.Testcases, tc)
		}
		suites = append(suites, s)
	}
	doc := struct {
		XMLName xml.Name         `xml:"testsuites"`
		Suites  []junitTestsuite `xml:"testsuite"`
	}{Suites: suites}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	return os.WriteFile(path, data, 0o600)
}
