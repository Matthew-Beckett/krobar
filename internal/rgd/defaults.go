// Copyright 2025 The Kubernetes Authors.
// Modified for krobar by the Krobar Authors.
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

package rgd

import "github.com/kubernetes-sigs/kro/pkg/graph"

// DefaultConfig mirrors the kro controller's default RGD collection limits
// (the rgd-max-collection-* flag defaults). It is shared by every krobar
// command that constructs a graph without user overrides.
var DefaultConfig = graph.RGDConfig{
	MaxCollectionSize:          1000,
	MaxCollectionDimensionSize: 10,
}
