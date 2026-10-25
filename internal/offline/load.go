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

package offline

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/yaml"
	openapiresolver "k8s.io/apiserver/pkg/cel/openapi/resolver"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	krograph "github.com/kubernetes-sigs/kro/pkg/graph"
)

// Set bundles the offline schema resolver and REST mapper the kro graph
// builder needs, plus the CRDs they were built from.
type Set struct {
	Resolver openapiresolver.SchemaResolver
	Mapper   meta.RESTMapper
	CRDs     []*apiextensionsv1.CustomResourceDefinition
}

// NewSet compiles an offline Set from CRDs.
func NewSet(crds []*apiextensionsv1.CustomResourceDefinition) (*Set, error) {
	resolver, err := newSchemaResolver(crds)
	if err != nil {
		return nil, err
	}
	return &Set{
		Resolver: resolver,
		Mapper:   newRESTMapper(crds),
		CRDs:     crds,
	}, nil
}

// NewSetBuilder returns a kro graph Builder wired to the set's offline
// resolver and REST mapper. It performs no cluster access.
func NewSetBuilder(set *Set) (*krograph.Builder, error) {
	return krograph.NewBuilder(nil, nil,
		krograph.WithSchemaResolver(set.Resolver),
		krograph.WithRESTMapper(set.Mapper),
	)
}

// RGDDoc is a ResourceGraphDefinition found while loading --crds paths,
// with its source path for error reporting.
type RGDDoc struct {
	RGD    *v1alpha1.ResourceGraphDefinition
	Source string
}

// LoadDocs reads every file or directory in paths (directories scanned
// recursively for *.yaml/*.yml/*.json), decodes multi-document YAML, and
// returns the CustomResourceDefinitions and ResourceGraphDefinitions found.
// All other documents are ignored. RGD docs are returned raw (still needing
// defaults applied and compilation by the caller).
func LoadDocs(paths []string) (crds []*apiextensionsv1.CustomResourceDefinition, rgds []RGDDoc, err error) {
	for _, p := range paths {
		err := walkDocs(p, func(doc []byte, source string) {
			switch docKind(doc) {
			case "CustomResourceDefinition":
				crd := &apiextensionsv1.CustomResourceDefinition{}
				if err := sigsyaml.Unmarshal(doc, crd); err == nil && crd.Spec.Group != "" {
					crds = append(crds, crd)
				}
			case "ResourceGraphDefinition":
				r := &v1alpha1.ResourceGraphDefinition{}
				if err := sigsyaml.Unmarshal(doc, r); err == nil {
					rgds = append(rgds, RGDDoc{RGD: r, Source: source})
				}
			}
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return crds, rgds, nil
}

// walkDocs calls fn for every YAML/JSON document under path.
func walkDocs(path string, fn func(doc []byte, source string)) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("--crds: %w", err)
	}

	visit := func(p string) error {
		data, err := os.ReadFile(p) // #nosec G304 -- user-supplied CLI argument
		if err != nil {
			return err
		}
		dec := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
		for {
			doc, err := dec.Read()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("reading %s: %w", p, err)
			}
			if len(bytes.TrimSpace(doc)) > 0 {
				fn(doc, p)
			}
		}
	}

	if !info.IsDir() {
		return visit(path)
	}
	return filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml", ".json":
			return visit(p)
		}
		return nil
	})
}

// docKind extracts just the kind field of a YAML/JSON doc, cheaply.
func docKind(doc []byte) string {
	var meta struct {
		Kind string `json:"kind"`
	}
	if err := sigsyaml.Unmarshal(doc, &meta); err != nil {
		return ""
	}
	return meta.Kind
}
