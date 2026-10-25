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

// Package version exposes build information stamped in at link time via
// -ldflags -X, with a runtime/debug fallback for `go install` builds.
package version

import (
	"fmt"
	"runtime/debug"
)

// Stamped at build time via:
//
//	-ldflags "-X github.com/Matthew-Beckett/krobar/internal/version.Version=v1.2.3 ..."
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// KubernetesVersion is the bundled Kubernetes schema version (fallback when
// the build info does not record the k8s.io/api module version). Keep in sync
// with the k8s.io/* dependency versions in go.mod.
const KubernetesVersion = "1.36"

// Info describes the build.
type Info struct {
	Version string
	Commit  string
	Date    string
	// Kubernetes is the Kubernetes minor version whose schemas are compiled
	// in (derived from the k8s.io/api dependency version).
	Kubernetes string
}

// Get returns build information. When Version was not stamped (the "dev" or
// empty default), it falls back to the module version recorded by the Go
// toolchain in debug.ReadBuildInfo.
func Get() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date, Kubernetes: KubernetesVersion}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if (info.Version == "" || info.Version == "dev") &&
			bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		for _, dep := range bi.Deps {
			if dep.Path == "k8s.io/api" {
				// v0.36.x → Kubernetes 1.36
				var minor int
				if n, _ := fmt.Sscanf(dep.Version, "v0.%d.", &minor); n == 1 {
					info.Kubernetes = fmt.Sprintf("1.%d", minor)
				}
			}
		}
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}
