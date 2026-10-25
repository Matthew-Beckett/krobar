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

// Package objects loads Kubernetes objects from YAML/JSON files and
// directories: multi-document streams, `kind: List` unwrapping, and the
// apiVersion/kind/namespace/name identity matching used by render and test.
package objects

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

// ReadFile reads all object documents from path ("-" reads r). Documents of
// kind "List" are unwrapped into their items.
func ReadFile(path string, r io.Reader) ([]*unstructured.Unstructured, error) {
	var src io.Reader
	if path == "-" {
		src = r
	} else {
		f, err := os.Open(path) // #nosec G304 -- user-supplied CLI argument
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		src = f
	}
	return decode(src)
}

// ReadPath reads objects from a file or, recursively in sorted order, from
// every *.yaml/*.yml/*.json file under a directory.
func ReadPath(path string) ([]*unstructured.Unstructured, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !info.IsDir() {
		return ReadFile(path, nil)
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml", ".json":
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []*unstructured.Unstructured
	for _, p := range files {
		objs, err := ReadFile(p, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, objs...)
	}
	return out, nil
}

// Decode is ReadFile for an in-memory document.
func Decode(data []byte) ([]*unstructured.Unstructured, error) {
	return decode(bytes.NewReader(data))
}

func decode(src io.Reader) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured
	dec := utilyaml.NewYAMLReader(bufio.NewReader(src))
	for {
		doc, err := dec.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		var obj map[string]interface{}
		if err := sigsyaml.Unmarshal(doc, &obj); err != nil {
			return nil, fmt.Errorf("invalid YAML document: %w", err)
		}
		if len(obj) == 0 {
			continue
		}
		if k, _, _ := unstructured.NestedString(obj, "kind"); k == "List" {
			items, found, err := unstructured.NestedSlice(obj, "items")
			if err != nil || !found {
				continue
			}
			for _, it := range items {
				if m, ok := it.(map[string]interface{}); ok && len(m) > 0 {
					out = append(out, &unstructured.Unstructured{Object: m})
				}
			}
			continue
		}
		out = append(out, &unstructured.Unstructured{Object: obj})
	}
}
