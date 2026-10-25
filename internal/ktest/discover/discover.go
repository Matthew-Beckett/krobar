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

// Package discover finds test suites and cases on the filesystem and
// resolves them into a run Plan: suite manifests (krobar-test.yaml),
// implicit suites (<dir>/tests next to a single RGD file), directory
// cases, and standalone TestCase documents.
package discover

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/Matthew-Beckett/krobar/internal/ktest/fieldpath"
	"github.com/Matthew-Beckett/krobar/internal/ktest/spec"
	"github.com/Matthew-Beckett/krobar/internal/objects"
)

// ManifestName is the suite manifest filename.
const ManifestName = "krobar-test.yaml"

// Plan is the result of discovery: the suites found under the given paths.
type Plan struct {
	Suites []*Suite
}

// Suite is a discovered test suite.
type Suite struct {
	Name   string
	Root   string
	RGD    string
	CRDs   []string
	Labels map[string]string
	Cases  []*Case
	// SnapshotMode is the resolved snapshot mode: strict, partial or off.
	SnapshotMode string
	// SnapshotIgnore holds the parsed spec.snapshot.ignore field paths.
	SnapshotIgnore []fieldpath.Path
	// Err, when non-nil, is a suite-level discovery error: the suite is
	// reported as failed and its cases are not run.
	Err error

	isSuite bool // manifest decoded as a TestSuite document
}

// Case is a discovered test case.
type Case struct {
	Name   string
	Dir    string
	File   string
	Labels map[string]string
	Skip   string
	// Steps holds exactly one implicit step (Name "").
	Steps []*Step
	// SnapshotDir is the case's snapshot directory; empty disables
	// snapshot handling.
	SnapshotDir string
	// SnapshotOff disables snapshot comparison and writing entirely.
	SnapshotOff bool
	// Err, when non-nil, is a fixture error: the case is reported failed.
	Err error
}

// Step is one simulated reconciliation within a case.
type Step struct {
	Name     string
	Instance *unstructured.Unstructured
	Observed []*unstructured.Unstructured
	Expect   *spec.Expect
	Assert   []spec.Assertion
}

// Path returns the suite's test path.
func (s *Suite) Path() string { return s.Name }

// Path returns the case's full test path, "suite/case".
func (c *Case) Path(s *Suite) string { return s.Name + "/" + c.Name }

// Discover resolves path arguments (directories or manifest files) into a
// Plan. Errors on the path arguments themselves are fatal; errors inside a
// suite or case are recorded on the returned Suite/Case.
func Discover(paths []string) (*Plan, error) {
	plan := &Plan{}
	protected := map[string]bool{}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if !info.IsDir() {
			s := suiteFromManifest(p)
			if s == nil {
				return nil, fmt.Errorf("%s: not a %s manifest", p, spec.KindSuite)
			}
			plan.Suites = append(plan.Suites, s)
			continue
		}
		err = filepath.WalkDir(p, func(d string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !de.IsDir() {
				return nil
			}
			// Skip hidden directories (.git, .hidden, ...) below the
			// argument root itself.
			if d != p && strings.HasPrefix(de.Name(), ".") {
				return fs.SkipDir
			}
			if protected[d] {
				return fs.SkipDir
			}
			if isFile(filepath.Join(d, ManifestName)) {
				plan.Suites = append(plan.Suites, explicitSuite(d))
				return fs.SkipDir
			}
			// Implicit suite: <d>/tests next to exactly one RGD file.
			testsDir := filepath.Join(d, "tests")
			if isDir(testsDir) && !isFile(filepath.Join(testsDir, ManifestName)) {
				if rgdFile, ok := singleRGDFile(d); ok {
					protected[testsDir] = true
					plan.Suites = append(plan.Suites, implicitSuite(d, testsDir, rgdFile))
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// suiteFromManifest builds a suite from an explicit manifest file
// argument. It returns nil when the file is not a TestSuite manifest.
func suiteFromManifest(file string) *Suite {
	s := &Suite{Root: filepath.Dir(file), Name: filepath.Base(filepath.Dir(file))}
	loadManifest(s, file)
	if !s.isSuite {
		return nil
	}
	return s
}

func explicitSuite(root string) *Suite {
	s := &Suite{Root: root, Name: filepath.Base(root)}
	loadManifest(s, filepath.Join(root, ManifestName))
	return s
}

// loadManifest parses the suite manifest into s (Name, RGD, CRDs, Labels,
// Err) and, on success, discovers the suite's cases.
func loadManifest(s *Suite, manifestPath string) {
	root := filepath.Dir(manifestPath)
	docs, err := readDocs(manifestPath)
	if err != nil {
		s.Err = err
		return
	}
	if len(docs) != 1 {
		s.Err = fmt.Errorf("%s must contain exactly one %s document", manifestPath, spec.KindSuite)
		return
	}
	doc, err := spec.Parse(docs[0])
	if err != nil {
		s.Err = err
		return
	}
	manifest, ok := doc.(*spec.Suite)
	if !ok {
		s.Err = fmt.Errorf("%s must contain a %s document", manifestPath, spec.KindSuite)
		return
	}
	s.isSuite = true
	if manifest.Metadata.Name != "" {
		s.Name = manifest.Metadata.Name
	}
	s.Labels = manifest.Metadata.Labels
	if err := manifest.Validate(); err != nil {
		s.Err = err
		return
	}
	if manifest.Spec.RGD == "" {
		s.Err = fmt.Errorf("spec.rgd is required")
		return
	}
	s.RGD = resolve(root, manifest.Spec.RGD)
	for _, c := range manifest.Spec.CRDs {
		s.CRDs = append(s.CRDs, resolve(root, c))
	}
	s.SnapshotMode = "strict"
	if sn := manifest.Spec.Snapshot; sn != nil {
		if sn.Mode != "" {
			s.SnapshotMode = sn.Mode
		}
		for _, p := range sn.Ignore {
			parsed, err := fieldpath.Parse(p)
			if err != nil {
				s.Err = fmt.Errorf("spec.snapshot.ignore: %w", err)
				return
			}
			s.SnapshotIgnore = append(s.SnapshotIgnore, parsed)
		}
	}
	s.discoverCases(manifest)
}

func implicitSuite(parent, testsDir, rgdFile string) *Suite {
	s := &Suite{
		Name:         filepath.Base(parent),
		Root:         testsDir,
		RGD:          rgdFile,
		SnapshotMode: "strict",
	}
	if isDir(filepath.Join(parent, "crds")) {
		s.CRDs = []string{filepath.Join(parent, "crds")}
	}
	s.discoverCases(nil)
	return s
}

// discoverCases finds the cases in the suite root, honouring the
// manifest's spec.cases glob filter (nil for implicit suites).
func (s *Suite) discoverCases(manifest *spec.Suite) {
	var globs []string
	if manifest != nil {
		globs = manifest.Spec.Cases
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		s.Err = fmt.Errorf("reading suite root: %w", err)
		return
	}
	seen := map[string]bool{}
	addCase := func(c *Case) {
		if seen[c.Name] {
			c.Err = fmt.Errorf("duplicate case name %q", c.Name)
		}
		seen[c.Name] = true
		s.Cases = append(s.Cases, c)
	}
	for _, e := range entries {
		if len(globs) > 0 {
			ok, err := matchAny(globs, e.Name())
			if err != nil {
				s.Err = err
				return
			}
			if !ok {
				continue
			}
		}
		if e.IsDir() {
			if e.Name() == "observed" || e.Name() == "expected" {
				continue
			}
			dir := filepath.Join(s.Root, e.Name())
			if isFile(filepath.Join(dir, "case.yaml")) ||
				isFile(filepath.Join(dir, "instance.yaml")) ||
				isDir(filepath.Join(dir, "observed")) {
				addCase(s.dirCase(dir, manifest))
			}
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".yaml", ".yml":
		default:
			continue
		}
		if e.Name() == ManifestName || e.Name() == "instance.yaml" {
			continue
		}
		s.fileCases(filepath.Join(s.Root, e.Name()), manifest, addCase)
		if s.Err != nil {
			return
		}
	}
}

// matchAny reports whether name matches any glob.
func matchAny(globs []string, name string) (bool, error) {
	for _, g := range globs {
		ok, err := filepath.Match(g, name)
		if err != nil {
			return false, fmt.Errorf("spec.cases: bad pattern %q: %w", g, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// dirCase builds the case rooted at dir (case.yaml / instance.yaml /
// observed/ conventions).
func (s *Suite) dirCase(dir string, manifest *spec.Suite) *Case {
	c := &Case{Name: filepath.Base(dir), Dir: dir}
	cs := &spec.CaseSpec{}
	caseFile := filepath.Join(dir, "case.yaml")
	if isFile(caseFile) {
		c.File = caseFile
		doc, parsedSpec, err := parseCaseFile(caseFile)
		if err != nil {
			c.Err = err
			return c
		}
		cs = parsedSpec
		if doc.Metadata.Name != "" {
			c.Name = doc.Metadata.Name
		}
		c.Labels = doc.Metadata.Labels
	}
	c.Skip = cs.Skip
	s.resolveSnapshot(c, cs, dir, true)
	if err := validateCaseSpec(cs); err != nil {
		c.Err = err
		return c
	}
	c.Err = s.buildStep(c, cs, manifest, dir, true)
	return c
}

// resolveSnapshot sets the case's snapshot directory from spec.snapshot,
// or the <dir>/expected convention for directory cases. A case-level
// "off" disables snapshot handling entirely; standalone-file cases have
// no convention and need spec.snapshot to opt in.
func (s *Suite) resolveSnapshot(c *Case, cs *spec.CaseSpec, fileDir string, dirCase bool) {
	if cs.Snapshot != nil {
		if *cs.Snapshot == "off" {
			c.SnapshotOff = true
			return
		}
		c.SnapshotDir = resolve(fileDir, *cs.Snapshot)
		return
	}
	if dirCase {
		c.SnapshotDir = filepath.Join(fileDir, "expected")
	}
}

// fileCases parses a standalone yaml file in the suite root; each TestCase
// document becomes one case.
func (s *Suite) fileCases(file string, manifest *spec.Suite, addCase func(*Case)) {
	docs, err := readDocs(file)
	if err != nil {
		s.Err = err
		return
	}
	base := fileBase(file)
	idx := 0
	for _, doc := range docs {
		if isLegacy(doc) {
			s.Err = fmt.Errorf("%s: RGDTest documents are no longer supported; convert to a TestSuite directory (see README \"Fixture tests\")", file)
			return
		}
		parsed, err := spec.Parse(doc)
		if err != nil {
			addCase(&Case{Name: numbered(base, idx), Dir: s.Root, File: file, Err: err})
			idx++
			continue
		}
		cd, ok := parsed.(*spec.Case)
		if !ok {
			continue
		}
		c := &Case{Name: cd.Metadata.Name, Dir: s.Root, File: file}
		if c.Name == "" {
			c.Name = numbered(base, idx)
			c.Err = fmt.Errorf("%s: TestCase document is missing metadata.name", file)
		}
		c.Labels = cd.Metadata.Labels
		c.Skip = cd.Spec.Skip
		if c.Err == nil {
			c.Err = cd.Validate()
		}
		if c.Err == nil {
			s.resolveSnapshot(c, &cd.Spec, filepath.Dir(file), false)
			c.Err = s.buildStep(c, &cd.Spec, manifest, filepath.Dir(file), false)
		}
		addCase(c)
		idx++
	}
}

func numbered(base string, idx int) string {
	if idx == 0 {
		return base
	}
	return fmt.Sprintf("%s#%d", base, idx)
}

// buildStep resolves the case's instance and observed objects into its
// single implicit step. fileDir is where a relative instanceFile
// resolves; dirCase enables the <dir>/instance.yaml and <dir>/observed/
// conventions.
func (s *Suite) buildStep(c *Case, cs *spec.CaseSpec, manifest *spec.Suite, fileDir string, dirCase bool) error {
	inst, err := s.resolveInstance(c, cs, manifest, fileDir, dirCase)
	if err != nil {
		return err
	}
	if len(cs.InstancePatch) > 0 {
		inst = inst.DeepCopy()
		inst.Object = mergePatch(inst.Object, cs.InstancePatch)
	}
	obs, err := s.resolveObserved(cs, fileDir, dirCase)
	if err != nil {
		return err
	}
	c.Steps = []*Step{{Instance: inst, Observed: obs, Expect: cs.Expect, Assert: cs.Assert}}
	return nil
}

// resolveInstance applies the case-level then suite-level instance rules.
func (s *Suite) resolveInstance(c *Case, cs *spec.CaseSpec, manifest *spec.Suite, fileDir string, dirCase bool) (*unstructured.Unstructured, error) {
	var inline map[string]interface{}
	var file string
	switch {
	case cs.Instance != nil && cs.InstanceFile != "":
		return nil, fmt.Errorf("spec.instance and spec.instanceFile are mutually exclusive")
	case cs.Instance != nil:
		inline = cs.Instance
	case cs.InstanceFile != "":
		file = resolve(fileDir, cs.InstanceFile)
	}
	convFile := filepath.Join(fileDir, "instance.yaml")
	convExists := dirCase && isFile(convFile)
	// An instanceFile resolving to the conventional path is the same
	// source, not a second one.
	fileIsConv := file != "" && convExists && filepath.Clean(file) == filepath.Clean(convFile)

	sources := 0
	for _, present := range []bool{inline != nil, file != "" && !fileIsConv, convExists} {
		if present {
			sources++
		}
	}
	switch {
	case sources > 1:
		return nil, fmt.Errorf("ambiguous instance: multiple instance sources at case level")
	case sources == 1:
		if inline != nil {
			return &unstructured.Unstructured{Object: deepCopyMap(inline)}, nil
		}
		if file == "" {
			file = convFile
		}
		return readInstanceFile(file)
	}

	var sInline map[string]interface{}
	var sFile string
	if manifest != nil {
		sInline = manifest.Spec.Instance
		if manifest.Spec.InstanceFile != "" {
			sFile = resolve(s.Root, manifest.Spec.InstanceFile)
		}
	}
	suiteConvFile := filepath.Join(s.Root, "instance.yaml")
	suiteConvExists := isFile(suiteConvFile)
	sFileIsConv := sFile != "" && suiteConvExists && filepath.Clean(sFile) == filepath.Clean(suiteConvFile)

	sources = 0
	for _, present := range []bool{sInline != nil, sFile != "" && !sFileIsConv, suiteConvExists} {
		if present {
			sources++
		}
	}
	switch {
	case sources > 1:
		return nil, fmt.Errorf("ambiguous instance: multiple instance sources at suite level")
	case sources == 0:
		return nil, fmt.Errorf("no instance: case %q defines none and the suite provides no default", c.Name)
	}
	if sInline != nil {
		return &unstructured.Unstructured{Object: deepCopyMap(sInline)}, nil
	}
	if sFile == "" {
		sFile = suiteConvFile
	}
	return readInstanceFile(sFile)
}

// resolveObserved merges suite <root>/observed/, case <dir>/observed/
// (dir cases only) and inline observed objects by identity; later sources
// replace earlier ones whole, keeping first-seen order.
func (s *Suite) resolveObserved(cs *spec.CaseSpec, fileDir string, dirCase bool) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured
	pos := map[string]int{}
	add := func(o *unstructured.Unstructured) {
		key := identity(o)
		if i, ok := pos[key]; ok {
			out[i] = o
			return
		}
		pos[key] = len(out)
		out = append(out, o)
	}
	suiteObs := filepath.Join(s.Root, "observed")
	if isDir(suiteObs) {
		objs, err := objects.ReadPath(suiteObs)
		if err != nil {
			return nil, fmt.Errorf("loading suite observed objects: %w", err)
		}
		for _, o := range objs {
			add(o)
		}
	}
	if dirCase {
		caseObs := filepath.Join(fileDir, "observed")
		if isDir(caseObs) {
			objs, err := objects.ReadPath(caseObs)
			if err != nil {
				return nil, fmt.Errorf("loading case observed objects: %w", err)
			}
			for _, o := range objs {
				add(o)
			}
		}
	}
	for _, m := range cs.Observed {
		add(&unstructured.Unstructured{Object: deepCopyMap(m)})
	}
	return out, nil
}

func identity(o *unstructured.Unstructured) string {
	return o.GetAPIVersion() + "\x00" + o.GetKind() + "\x00" + o.GetNamespace() + "\x00" + o.GetName()
}

func readInstanceFile(path string) (*unstructured.Unstructured, error) {
	docs, err := objects.ReadFile(path, nil)
	if err != nil {
		return nil, fmt.Errorf("loading instance file: %w", err)
	}
	if len(docs) != 1 {
		return nil, fmt.Errorf("instance file must contain exactly one document, got %d", len(docs))
	}
	return docs[0], nil
}

// parseCaseFile reads case.yaml: exactly one TestCase document.
func parseCaseFile(path string) (*spec.Case, *spec.CaseSpec, error) {
	docs, err := readDocs(path)
	if err != nil {
		return nil, nil, err
	}
	var found *spec.Case
	for _, doc := range docs {
		parsed, err := spec.Parse(doc)
		if err != nil {
			return nil, nil, err
		}
		if cd, ok := parsed.(*spec.Case); ok {
			if found != nil {
				return nil, nil, fmt.Errorf("%s: multiple TestCase documents", path)
			}
			found = cd
		}
	}
	if found == nil {
		return nil, nil, fmt.Errorf("%s: no TestCase document", path)
	}
	return found, &found.Spec, nil
}

func validateCaseSpec(cs *spec.CaseSpec) error {
	c := &spec.Case{Spec: *cs}
	return c.Validate()
}

// isLegacy probes a document for the legacy RGDTest kind.
func isLegacy(doc []byte) bool {
	var probe struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := sigsyaml.Unmarshal(doc, &probe); err != nil {
		return false
	}
	return probe.APIVersion == spec.APIVersion && probe.Kind == "RGDTest"
}

// readDocs splits a YAML file into its documents.
func readDocs(path string) ([][]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- user-supplied CLI argument
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out [][]byte
	dec := utilyaml.NewYAMLReader(bufio.NewReader(f))
	for {
		doc, err := dec.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(bytes.TrimSpace(doc)) > 0 {
			out = append(out, doc)
		}
	}
}

// singleRGDFile reports whether dir has exactly one top-level yaml file
// whose kind is ResourceGraphDefinition, returning its path.
func singleRGDFile(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var found string
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch filepath.Ext(e.Name()) {
		case ".yaml", ".yml":
		default:
			continue
		}
		if isRGD(filepath.Join(dir, e.Name())) {
			found = filepath.Join(dir, e.Name())
			count++
		}
	}
	return found, count == 1
}

func isRGD(path string) bool {
	docs, err := readDocs(path)
	if err != nil || len(docs) == 0 {
		return false
	}
	var probe struct {
		Kind string `json:"kind"`
	}
	return sigsyaml.Unmarshal(docs[0], &probe) == nil && probe.Kind == "ResourceGraphDefinition"
}

func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

func fileBase(path string) string {
	b := filepath.Base(path)
	return b[:len(b)-len(filepath.Ext(b))]
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
