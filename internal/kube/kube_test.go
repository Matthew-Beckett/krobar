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

package kube

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: one
  cluster:
    server: https://one.example.com
- name: two
  cluster:
    server: https://two.example.com
contexts:
- name: ctx-one
  context:
    cluster: one
    user: u
- name: ctx-two
  context:
    cluster: two
    user: u
current-context: ctx-one
users:
- name: u
  user:
    token: fake
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(testKubeconfig), 0o600))
	return path
}

func TestRESTConfigExplicitMissingPath(t *testing.T) {
	_, err := RESTConfig(filepath.Join(t.TempDir(), "does-not-exist"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load Kubernetes configuration")
}

func TestRESTConfigContextOverride(t *testing.T) {
	path := writeKubeconfig(t)

	cfg, err := RESTConfig(path, "")
	require.NoError(t, err)
	assert.Equal(t, "https://one.example.com", cfg.Host)

	cfg, err = RESTConfig(path, "ctx-two")
	require.NoError(t, err)
	assert.Equal(t, "https://two.example.com", cfg.Host)
}

func TestRESTConfigBadContext(t *testing.T) {
	_, err := RESTConfig(writeKubeconfig(t), "no-such-context")
	require.Error(t, err)
}
