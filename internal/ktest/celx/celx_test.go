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

package celx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func eval(t *testing.T, expr string, vars Vars) (bool, error) {
	t.Helper()
	p, err := Compile(expr)
	require.NoError(t, err)
	return Eval(p, vars)
}

func TestCompileAndEval(t *testing.T) {
	ok, err := eval(t, `self.image == "nginx"`, Vars{
		Self: map[string]interface{}{"image": "nginx"},
	})
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = eval(t, `self.image == "nginx"`, Vars{
		Self: map[string]interface{}{"image": "alpine"},
	})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestNilVars(t *testing.T) {
	ok, err := eval(t, `self == null && instance == null && status == null &&
		nodes == {} && vars == {}`, Vars{})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestNonBoolOutputRejected(t *testing.T) {
	_, err := Compile(`"a string"`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "want bool")

	p, err := Compile(`dyn("x")`)
	require.NoError(t, err)
	_, err = Eval(p, Vars{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "want bool")
}

func TestCrossTypeNumeric(t *testing.T) {
	// YAML-decoded values are float64; rendered objects carry int64.
	ok, err := eval(t, `self.replicas == instance.replicas`, Vars{
		Self:     map[string]interface{}{"replicas": int64(3)},
		Instance: map[string]interface{}{"replicas": 3.0},
	})
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = eval(t, `self.count < instance.limit`, Vars{
		Self:     map[string]interface{}{"count": int64(2)},
		Instance: map[string]interface{}{"limit": 2.5},
	})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestEvalError(t *testing.T) {
	p, err := Compile(`self.list[10] > 0`)
	require.NoError(t, err)
	_, err = Eval(p, Vars{Self: map[string]interface{}{"list": []interface{}{}}})
	assert.Error(t, err)
}

func TestCompileError(t *testing.T) {
	_, err := Compile(`self.`)
	assert.Error(t, err)
}
