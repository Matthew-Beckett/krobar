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

// Package report implements run.Sink outputs: a go-test-style text report
// and a JUnit XML report.
package report

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Matthew-Beckett/krobar/internal/ktest/run"
)

// Text prints go-test-style result lines for each terminal event.
type Text struct {
	W io.Writer
}

// Event handles a run event: terminal actions print a result line.
func (t *Text) Event(ev run.Event) {
	switch ev.Action {
	case run.ActionPass:
		_, _ = fmt.Fprintf(t.W, "--- PASS: %s (%.2fs)\n", ev.Path, ev.Elapsed.Seconds())
		t.printInfo(ev)
	case run.ActionFail:
		_, _ = fmt.Fprintf(t.W, "--- FAIL: %s (%.2fs)\n", ev.Path, ev.Elapsed.Seconds())
		for _, m := range ev.Messages {
			for _, line := range strings.Split(m, "\n") {
				_, _ = fmt.Fprintf(t.W, "    %s\n", line)
			}
		}
		t.printInfo(ev)
	case run.ActionSkip:
		reason := ""
		if len(ev.Messages) > 0 {
			reason = " (" + strings.Join(ev.Messages, "; ") + ")"
		}
		_, _ = fmt.Fprintf(t.W, "--- SKIP: %s (%.2fs)%s\n", ev.Path, ev.Elapsed.Seconds(), reason)
	}
}

// printInfo writes an event's informational lines indented under its
// result line.
func (t *Text) printInfo(ev run.Event) {
	for _, m := range ev.Info {
		for _, line := range strings.Split(m, "\n") {
			_, _ = fmt.Fprintf(t.W, "    %s\n", line)
		}
	}
}

// Finish prints the summary line.
func (t *Text) Finish(s run.Summary) {
	if s.Failed == 0 {
		_, _ = fmt.Fprintf(t.W, "ok\t%d passed", s.Passed)
	} else {
		_, _ = fmt.Fprintf(t.W, "FAIL\t%d of %d failed", s.Failed, s.Passed+s.Failed)
	}
	if s.Skipped > 0 {
		_, _ = fmt.Fprintf(t.W, ", %d skipped", s.Skipped)
	}
	_, _ = fmt.Fprintln(t.W)
}

// JUnit collects events and writes a JUnit XML report.
type JUnit struct {
	suites []junitSuite
	order  map[string]int
}

type junitSuite struct {
	name  string
	cases []junitCase
}

type junitCase struct {
	event run.Event
}

// Event records terminal events, grouped by suite in first-seen order.
func (j *JUnit) Event(ev run.Event) {
	if ev.Action == run.ActionRun {
		return
	}
	if j.order == nil {
		j.order = map[string]int{}
	}
	i, ok := j.order[ev.Suite]
	if !ok {
		i = len(j.suites)
		j.order[ev.Suite] = i
		j.suites = append(j.suites, junitSuite{name: ev.Suite})
	}
	j.suites[i].cases = append(j.suites[i].cases, junitCase{event: ev})
}

// JUnit XML structures.
type xmlFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}
type xmlSkipped struct {
	Message string `xml:"message,attr"`
}
type xmlTestcase struct {
	Name      string      `xml:"name,attr"`
	Classname string      `xml:"classname,attr"`
	File      string      `xml:"file,attr"`
	Time      string      `xml:"time,attr"`
	Failure   *xmlFailure `xml:"failure,omitempty"`
	Skipped   *xmlSkipped `xml:"skipped,omitempty"`
}
type xmlTestsuite struct {
	Name      string        `xml:"name,attr"`
	Tests     int           `xml:"tests,attr"`
	Failures  int           `xml:"failures,attr"`
	Skipped   int           `xml:"skipped,attr"`
	Time      string        `xml:"time,attr"`
	Testcases []xmlTestcase `xml:"testcase"`
}
type xmlTestsuites struct {
	XMLName xml.Name       `xml:"testsuites"`
	Suites  []xmlTestsuite `xml:"testsuite"`
}

// Write marshals the collected events to path (perms 0600).
func (j *JUnit) Write(path string) error {
	doc := xmlTestsuites{}
	for _, s := range j.suites {
		ts := xmlTestsuite{Name: s.name}
		var elapsed float64
		for _, c := range s.cases {
			ev := c.event
			ts.Tests++
			elapsed += ev.Elapsed.Seconds()
			name := ev.Case
			if name == "" {
				name = "(suite)"
			}
			tc := xmlTestcase{
				Name:      name,
				Classname: s.name,
				File:      ev.File,
				Time:      fmt.Sprintf("%.3f", ev.Elapsed.Seconds()),
			}
			switch ev.Action {
			case run.ActionFail:
				ts.Failures++
				tc.Failure = &xmlFailure{Message: "expectation failed", Text: strings.Join(ev.Messages, "\n")}
			case run.ActionSkip:
				ts.Skipped++
				tc.Skipped = &xmlSkipped{Message: strings.Join(ev.Messages, "; ")}
			}
			ts.Testcases = append(ts.Testcases, tc)
		}
		ts.Time = fmt.Sprintf("%.3f", elapsed)
		doc.Suites = append(doc.Suites, ts)
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	return os.WriteFile(path, data, 0o600)
}

var (
	_ run.Sink = (*Text)(nil)
	_ run.Sink = (*JUnit)(nil)
)
