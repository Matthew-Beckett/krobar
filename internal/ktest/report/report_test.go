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

package report

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Matthew-Beckett/krobar/internal/ktest/run"
)

func TestTextOutput(t *testing.T) {
	var buf bytes.Buffer
	text := &Text{W: &buf}
	text.Event(run.Event{Path: "s/a", Action: run.ActionRun})
	text.Event(run.Event{Path: "s/a", Action: run.ActionPass})
	text.Event(run.Event{Path: "s/b", Action: run.ActionRun})
	text.Event(run.Event{Path: "s/b", Action: run.ActionFail, Messages: []string{"msg one", "msg two"}})
	text.Event(run.Event{Path: "s/c", Action: run.ActionRun})
	text.Event(run.Event{Path: "s/c", Action: run.ActionSkip, Messages: []string{"not ready"}})
	text.Finish(run.Summary{Passed: 1, Failed: 1, Skipped: 1})

	want := "--- PASS: s/a (0.00s)\n" +
		"--- FAIL: s/b (0.00s)\n" +
		"    msg one\n" +
		"    msg two\n" +
		"--- SKIP: s/c (0.00s) (not ready)\n" +
		"FAIL\t1 of 2 failed, 1 skipped\n"
	assert.Equal(t, want, buf.String())
}

func TestTextMultilineMessages(t *testing.T) {
	var buf bytes.Buffer
	text := &Text{W: &buf}
	text.Event(run.Event{Path: "s/a", Action: run.ActionFail,
		Messages: []string{"first line\nsecond line"}})
	assert.Equal(t, "--- FAIL: s/a (0.00s)\n    first line\n    second line\n", buf.String())
}

func TestTextFinishOk(t *testing.T) {
	var buf bytes.Buffer
	text := &Text{W: &buf}
	text.Finish(run.Summary{Passed: 3})
	assert.Equal(t, "ok\t3 passed\n", buf.String())
}

func TestJUnitWrite(t *testing.T) {
	j := &JUnit{}
	j.Event(run.Event{Suite: "s1", Case: "a", Path: "s1/a", File: "f1", Action: run.ActionRun})
	j.Event(run.Event{Suite: "s1", Case: "a", Path: "s1/a", File: "f1", Action: run.ActionPass, Elapsed: 1500000000})
	j.Event(run.Event{Suite: "s1", Case: "b", Path: "s1/b", Action: run.ActionFail, Messages: []string{"m1", "m2"}})
	j.Event(run.Event{Suite: "s2", Case: "", Path: "s2", Action: run.ActionFail, Messages: []string{"suite error: bad"}})
	j.Event(run.Event{Suite: "s1", Case: "c", Path: "s1/c", Action: run.ActionSkip, Messages: []string{"later"}})

	path := filepath.Join(t.TempDir(), "junit.xml")
	require.NoError(t, j.Write(path))
	data, err := os.ReadFile(path) // #nosec G304 -- test file
	require.NoError(t, err)

	var doc struct {
		Suites []struct {
			Name     string `xml:"name,attr"`
			Tests    int    `xml:"tests,attr"`
			Failures int    `xml:"failures,attr"`
			Skipped  int    `xml:"skipped,attr"`
			Time     string `xml:"time,attr"`
			Cases    []struct {
				Name      string `xml:"name,attr"`
				Classname string `xml:"classname,attr"`
				File      string `xml:"file,attr"`
				Time      string `xml:"time,attr"`
				Failure   *struct {
					Message string `xml:"message,attr"`
					Text    string `xml:",chardata"`
				} `xml:"failure"`
				Skipped *struct {
					Message string `xml:"message,attr"`
				} `xml:"skipped"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	require.NoError(t, xml.Unmarshal(data, &doc))
	require.Len(t, doc.Suites, 2)

	s1 := doc.Suites[0]
	assert.Equal(t, "s1", s1.Name)
	assert.Equal(t, 3, s1.Tests)
	assert.Equal(t, 1, s1.Failures)
	assert.Equal(t, 1, s1.Skipped)
	require.Len(t, s1.Cases, 3)
	assert.Equal(t, "a", s1.Cases[0].Name)
	assert.Equal(t, "s1", s1.Cases[0].Classname)
	assert.Equal(t, "f1", s1.Cases[0].File)
	assert.Equal(t, "1.500", s1.Cases[0].Time)
	assert.Nil(t, s1.Cases[0].Failure)
	require.NotNil(t, s1.Cases[1].Failure)
	assert.Equal(t, "expectation failed", s1.Cases[1].Failure.Message)
	assert.Equal(t, "m1\nm2", s1.Cases[1].Failure.Text)
	require.NotNil(t, s1.Cases[2].Skipped)
	assert.Equal(t, "later", s1.Cases[2].Skipped.Message)

	s2 := doc.Suites[1]
	assert.Equal(t, "s2", s2.Name)
	assert.Equal(t, 1, s2.Tests)
	assert.Equal(t, 1, s2.Failures)
	require.Len(t, s2.Cases, 1)
	assert.Equal(t, "(suite)", s2.Cases[0].Name)
}
