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

package testcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Matthew-Beckett/krobar/internal/cli/clienv"
	"github.com/Matthew-Beckett/krobar/internal/exitcode"
)

// TestRunsShippedFixtures executes the RGDTest fixtures under
// testdata/tests through the real command (offline builder, no cluster).
func TestRunsShippedFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "tests")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("testdata/tests not present")
	}
	cmd := NewCommand(clienv.Default(), &clienv.Flags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "ok\t")
	assert.NotContains(t, out.String(), "--- FAIL")
}

func TestZeroTestsIsError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n"), 0o600))
	cmd := NewCommand(clienv.Default(), &clienv.Flags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{dir})
	err := cmd.Execute()
	require.Error(t, err)
	var ec *exitcode.Error
	require.ErrorAs(t, err, &ec)
	assert.Equal(t, exitcode.Failed, ec.Code)
	assert.Contains(t, err.Error(), "no RGDTest documents found")
}
