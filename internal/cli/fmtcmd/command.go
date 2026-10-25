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

package fmtcmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Matthew-Beckett/krobar/internal/exitcode"
)

// NewCommand constructs the `krobar fmt` command.
func NewCommand() *cobra.Command {
	var files []string
	var check bool

	cmd := &cobra.Command{
		Use:   "fmt [PATH...]",
		Short: "Format YAML manifest files canonically",
		Long: "Format YAML files with 2-space indentation, preserving comments " +
			"and multi-document files. ResourceGraphDefinition documents get " +
			"canonical key ordering.\n\n" +
			"Paths may be files or directories (scanned recursively for " +
			"*.yaml/*.yml). Files are rewritten in place; \"-\" reads stdin " +
			"and writes stdout. --check reports files needing formatting " +
			"without writing (exit code 2 if any).",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := append(append([]string{}, files...), args...)
			if len(paths) == 0 {
				return fmt.Errorf("at least one file or directory is required (or - for stdin)")
			}
			return run(cmd, paths, check)
		},
	}
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Files or directories to format (repeatable)")
	cmd.Flags().BoolVar(&check, "check", false, "Only report files that need formatting; write nothing")
	return cmd
}

func run(cmd *cobra.Command, paths []string, check bool) error {
	var needsFormatting []string

	for _, p := range paths {
		if p == "-" {
			if check {
				return fmt.Errorf("--check cannot be used with stdin")
			}
			data, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return err
			}
			formatted, err := Format(data)
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(formatted); err != nil {
				return err
			}
			continue
		}
		if err := walk(p, func(file string) error {
			changed, err := formatFile(file, check)
			if err != nil {
				return err
			}
			if changed {
				needsFormatting = append(needsFormatting, file)
				if check {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), file)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	if check && len(needsFormatting) > 0 {
		return exitcode.New(exitcode.Findings, fmt.Errorf("%d file(s) need formatting", len(needsFormatting)))
	}
	return nil
}

// walk applies fn to file, or recursively to *.yaml/*.yml under a directory.
func walk(path string, fn func(string) error) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fn(path)
	}
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml":
			return fn(p)
		}
		return nil
	})
}

// formatFile formats one file in place (unless check). Returns whether the
// file's content changed/would change.
func formatFile(path string, check bool) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied CLI argument
	if err != nil {
		return false, err
	}
	formatted, err := Format(data)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if string(formatted) == string(data) {
		return false, nil
	}
	if check {
		return true, nil
	}
	return true, os.WriteFile(path, formatted, info.Mode())
}
