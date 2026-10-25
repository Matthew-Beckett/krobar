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

// Package output serialises objects to yaml or json on a writer.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"
)

// Format is an output serialisation format.
type Format string

const (
	// FormatYAML marshals via sigs.k8s.io/yaml so json struct tags are honoured.
	FormatYAML Format = "yaml"
	// FormatJSON marshals via encoding/json with two-space indentation.
	FormatJSON Format = "json"
)

// ParseFormat validates s as an output format.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatYAML, FormatJSON:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unsupported output format: %s", s)
	}
}

// Write marshals obj in the given format and writes it to w with exactly one
// trailing newline.
func Write(w io.Writer, obj any, format Format) error {
	if obj == nil {
		return fmt.Errorf("nil object provided for marshaling")
	}

	var b []byte
	var err error
	switch format {
	case FormatJSON:
		b, err = json.MarshalIndent(obj, "", "  ")
	case FormatYAML:
		b, err = yaml.Marshal(obj)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
	if err != nil {
		return err
	}

	b = bytes.TrimRight(b, "\n")
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
