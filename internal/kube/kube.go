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

// Package kube resolves Kubernetes REST configuration using the standard
// client-go loading rules.
package kube

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// RESTConfig resolves a *rest.Config honouring the standard kubeconfig loading
// rules (KUBECONFIG env var, then ~/.kube/config). When kubeconfig is non-empty
// it is used as the explicit path; when context is non-empty it overrides the
// current context. If no kubeconfig can be loaded it falls back to in-cluster
// configuration.
func RESTConfig(kubeconfig, context string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}

	overrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		overrides.CurrentContext = context
	}

	// NewNonInteractiveDeferredLoadingClientConfig already falls back to
	// in-cluster configuration when no kubeconfig is found.
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load Kubernetes configuration: %w", err)
	}
	return cfg, nil
}
