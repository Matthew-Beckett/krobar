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
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// clusterScopedKinds lists built-in kinds that are cluster-scoped. Everything
// else in the
// compiled-in scheme is treated as namespaced.
var clusterScopedKinds = map[string]bool{
	"Namespace":                        true,
	"Node":                             true,
	"PersistentVolume":                 true,
	"ComponentStatus":                  true,
	"ClusterRole":                      true,
	"ClusterRoleBinding":               true,
	"StorageClass":                     true,
	"CSIDriver":                        true,
	"CSINode":                          true,
	"VolumeAttachment":                 true,
	"VolumeAttributesClass":            true,
	"CustomResourceDefinition":         true,
	"APIService":                       true,
	"PriorityClass":                    true,
	"RuntimeClass":                     true,
	"IngressClass":                     true,
	"IPAddress":                        true,
	"ServiceCIDR":                      true,
	"CertificateSigningRequest":        true,
	"ClusterTrustBundle":               true,
	"ValidatingWebhookConfiguration":   true,
	"MutatingWebhookConfiguration":     true,
	"ValidatingAdmissionPolicy":        true,
	"ValidatingAdmissionPolicyBinding": true,
	"MutatingAdmissionPolicy":          true,
	"MutatingAdmissionPolicyBinding":   true,
	"FlowSchema":                       true,
	"PriorityLevelConfiguration":       true,
	"DeviceClass":                      true,
	"ResourceSlice":                    true,
	"StorageVersion":                   true,
	"StorageVersionMigration":          true,
	"TokenReview":                      true,
	"SubjectAccessReview":              true,
	"SelfSubjectAccessReview":          true,
	"SelfSubjectRulesReview":           true,
	"SelfSubjectReview":                true,
	"ClusterCIDR":                      true,
}

// isResourceKind reports whether kind names a REST resource rather than a
// list, options, or meta type.
func isResourceKind(kind string) bool {
	if strings.HasSuffix(kind, "List") || strings.HasSuffix(kind, "Options") {
		return false
	}
	switch kind {
	case "Status", "WatchEvent", "APIGroup", "APIVersions",
		"APIResourceList", "DeleteOptions", "ExportOptions":
		return false
	}
	return true
}

// newRESTMapper builds a RESTMapper covering every built-in type in the
// client-go scheme plus every served version of the given CRDs. Scopes come
// from clusterScopedKinds for built-ins and spec.scope for CRDs; plurals are
// guessed with meta.UnsafeGuessKindToResource (or spec.names.plural for CRDs).
func newRESTMapper(crds []*apiextensionsv1.CustomResourceDefinition) meta.RESTMapper {
	m := meta.NewDefaultRESTMapper(nil)

	for gvk := range Scheme.AllKnownTypes() {
		if gvk.Version == "" || strings.HasPrefix(gvk.Version, "__") {
			continue // internal versions
		}
		if !isResourceKind(gvk.Kind) {
			continue
		}
		scope := meta.RESTScopeNamespace
		if clusterScopedKinds[gvk.Kind] {
			scope = meta.RESTScopeRoot
		}
		m.Add(gvk, scope)
	}

	for _, crd := range crds {
		scope := meta.RESTScopeNamespace
		if crd.Spec.Scope == apiextensionsv1.ClusterScoped {
			scope = meta.RESTScopeRoot
		}
		for _, v := range crd.Spec.Versions {
			if !v.Served {
				continue
			}
			m.AddSpecific(
				schema.GroupVersionKind{
					Group:   crd.Spec.Group,
					Version: v.Name,
					Kind:    crd.Spec.Names.Kind,
				},
				schema.GroupVersionResource{
					Group:    crd.Spec.Group,
					Version:  v.Name,
					Resource: crd.Spec.Names.Plural,
				},
				schema.GroupVersionResource{
					Group:    crd.Spec.Group,
					Version:  v.Name,
					Resource: crd.Spec.Names.Singular,
				},
				scope,
			)
		}
	}

	return m
}
