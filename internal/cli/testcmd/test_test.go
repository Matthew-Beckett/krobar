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

// TestRunsShippedFixtures executes the suites under testdata/tests through
// the real command (offline builder, no cluster).
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
	for _, path := range []string{
		"webapp/defaults", "webapp/ready", "webapp/ingress",
		"webapp/invalid-instance", "webapp/assertions", "vpc/renders",
	} {
		assert.Contains(t, out.String(), "--- PASS: "+path)
	}
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
	assert.Contains(t, err.Error(), "no tests found under")
}

func TestRunFilterNoMatch(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "tests")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("testdata/tests not present")
	}
	cmd := NewCommand(clienv.Default(), &clienv.Flags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--run", "no-such-test", dir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no tests matched --run "no-such-test"`)
}

func TestRunFilterSelects(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "tests")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("testdata/tests not present")
	}
	cmd := NewCommand(clienv.Default(), &clienv.Flags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--run", "webapp/ready", dir})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "--- PASS: webapp/ready")
	assert.NotContains(t, out.String(), "vpc/renders")
}

func TestUpdateSnapshotsFlag(t *testing.T) {
	cmd := NewCommand(clienv.Default(), &clienv.Flags{})
	f := cmd.Flags().Lookup("update-snapshots")
	require.NotNil(t, f)
	assert.Equal(t, "u", f.Shorthand)
	require.NoError(t, cmd.ParseFlags([]string{"-u"}))
	assert.Equal(t, "true", f.Value.String())
}

// TestUpdateSnapshotsSmoke copies the webapp suite to a temp dir, writes
// its snapshots with -u, then checks a normal run compares them clean.
func TestUpdateSnapshotsSmoke(t *testing.T) {
	src := filepath.Join("..", "..", "..", "testdata")
	if _, err := os.Stat(src); err != nil {
		t.Skip("testdata not present")
	}
	tmp := t.TempDir()
	rgd, err := os.ReadFile(filepath.Join(src, "webapp-rgd.yaml")) // #nosec G304 -- test
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "webapp-rgd.yaml"), rgd, 0o600)) // #nosec G703 -- test temp dir
	dir := filepath.Join(tmp, "tests", "webapp")
	require.NoError(t, os.CopyFS(dir, os.DirFS(filepath.Join(src, "tests", "webapp"))))
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "defaults", "expected")))

	cmd := NewCommand(clienv.Default(), &clienv.Flags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-u", dir})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "snapshot: wrote")
	assert.FileExists(t, filepath.Join(dir, "defaults", "expected", "deployment.yaml"))
	assert.FileExists(t, filepath.Join(dir, "defaults", "expected", "_states.yaml"))

	cmd = NewCommand(clienv.Default(), &clienv.Flags{})
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "--- PASS: webapp/defaults")
	assert.NotContains(t, out.String(), "--- FAIL")
}

func TestJUnitFileWritten(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "testdata", "tests")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("testdata/tests not present")
	}
	junit := filepath.Join(t.TempDir(), "report.xml")
	cmd := NewCommand(clienv.Default(), &clienv.Flags{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--junit", junit, dir})
	require.NoError(t, cmd.Execute())
	data, err := os.ReadFile(junit) // #nosec G304 -- test file
	require.NoError(t, err)
	assert.Contains(t, string(data), "<testsuites>")
	assert.Contains(t, string(data), `name="webapp"`)
}
