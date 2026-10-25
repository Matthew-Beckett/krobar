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

// Package celx compiles and evaluates the CEL expressions used by test
// assertions against a shared environment.
package celx

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
)

// Vars carries the activations available to an expression. Nil Self,
// Instance and Status fields are bound as null; nil Nodes and Vars are
// bound as empty maps.
type Vars struct {
	Self     interface{}
	Instance interface{}
	Status   interface{}
	Nodes    interface{}
	Vars     interface{}
}

var (
	envOnce sync.Once
	env     *cel.Env
	envErr  error
)

func environment() (*cel.Env, error) {
	envOnce.Do(func() {
		env, envErr = cel.NewEnv(
			cel.Variable("self", cel.DynType),
			cel.Variable("instance", cel.DynType),
			cel.Variable("status", cel.DynType),
			cel.Variable("nodes", cel.DynType),
			cel.Variable("vars", cel.DynType),
			ext.Strings(),
			ext.Lists(),
			ext.Sets(),
			ext.Math(),
			ext.Encoders(),
			cel.OptionalTypes(),
			cel.CrossTypeNumericComparisons(true),
		)
	})
	return env, envErr
}

// Compile checks expr and returns a program. The expression's output type
// must be bool or dyn.
func Compile(expr string) (cel.Program, error) {
	e, err := environment()
	if err != nil {
		return nil, err
	}
	ast, iss := e.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}
	if out := ast.OutputType(); out != cel.BoolType && out != cel.DynType {
		return nil, fmt.Errorf("expression has type %s, want bool", out)
	}
	return e.Program(ast)
}

// Eval runs the program with the given activations and requires a bool
// result.
func Eval(p cel.Program, vars Vars) (bool, error) {
	act := map[string]interface{}{
		"self":     null(vars.Self),
		"instance": null(vars.Instance),
		"status":   null(vars.Status),
		"nodes":    orEmptyMap(vars.Nodes),
		"vars":     orEmptyMap(vars.Vars),
	}
	out, _, err := p.Eval(act)
	if err != nil {
		return false, err
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("expression returned %s, want bool", out.Type())
	}
	return b, nil
}

func null(v interface{}) interface{} {
	if v == nil {
		return types.NullValue
	}
	return v
}

func orEmptyMap(v interface{}) interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}
