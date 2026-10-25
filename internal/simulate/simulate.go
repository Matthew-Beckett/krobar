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

// Package simulate drives kro's real runtime (pkg/runtime) the way the
// instance controller does, but against user-supplied observed objects
// instead of a cluster: desired objects are resolved from the graph, an
// observed object is synthesised as (desired + user overlay) for each node,
// and readiness/instance status are computed with the same runtime calls.
package simulate

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	krograph "github.com/kubernetes-sigs/kro/pkg/graph"
	kroruntime "github.com/kubernetes-sigs/kro/pkg/runtime"

	"github.com/Matthew-Beckett/krobar/internal/instance"
	"github.com/Matthew-Beckett/krobar/internal/rgd"
)

// State is the terminal state of a simulated node.
type State string

const (
	// StateReady means the node's observed object satisfied CheckReadiness.
	StateReady State = "Ready"
	// StateNotReady means the node resolved but CheckReadiness failed.
	StateNotReady State = "NotReady"
	// StatePending means desired resolution is data-pending (a dependency's
	// data is not yet available, or an external ref needs an observed object).
	StatePending State = "Pending"
	// StateIgnored means the node's includeWhen evaluated to false.
	StateIgnored State = "Ignored"
)

// NodeResult is the outcome of simulating one resource node.
type NodeResult struct {
	ID      string
	State   State
	Reason  string
	Objects []*unstructured.Unstructured
}

// Result is the outcome of a simulation run.
type Result struct {
	// Nodes is in graph topological order.
	Nodes []NodeResult
	// Instance is the defaulted instance with the runtime-computed .status.
	Instance *unstructured.Unstructured
	// UnmatchedObserved holds user-supplied observed objects that matched no
	// node's desired object(s).
	UnmatchedObserved []*unstructured.Unstructured
}

// Run simulates one reconciliation of the given instance against the graph.
// inst is mutated neither by defaulting nor status projection (a deep copy
// is used). observed supplies the objects the simulated "cluster" already
// contains; each is matched to a node's desired object by apiVersion, kind,
// namespace and name and merged on top of the desired object.
func Run(g *krograph.Graph, inst *unstructured.Unstructured, observed []*unstructured.Unstructured) (*Result, error) {
	work := inst.DeepCopy()

	// Like the kro instance controller, the instance is defaulted and
	// validated as the API server would on write, so the runtime sees the
	// same object it would see in a real reconcile.
	if g.CRD != nil {
		s, err := instance.NewSchema(g.CRD, "")
		if err != nil {
			return nil, fmt.Errorf("cannot build instance schema: %w", err)
		}
		s.Default(work.Object)
		if errs := s.Validate(context.Background(), work.Object); len(errs) > 0 {
			return nil, fmt.Errorf("instance is invalid: %w", errs.ToAggregate())
		}
	}

	rt, err := kroruntime.FromGraph(g, work, rgd.DefaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime: %w", err)
	}

	matched := make(map[*unstructured.Unstructured]bool, len(observed))
	res := &Result{Instance: work}

	// Nodes are processed in topological order; each node's observed state
	// is fed back via SetObserved so dependents can resolve.
	for _, node := range rt.Nodes() {
		nr := NodeResult{ID: node.Spec.Meta.ID}

		// includeWhen is evaluated before desired state is resolved.
		ignored, err := node.IsIgnored()
		switch {
		case err != nil && kroruntime.IsDataPending(err):
			nr.State, nr.Reason = StatePending, pendingReason("includeWhen", err)
		case err != nil:
			return nil, fmt.Errorf("node %q: %w", nr.ID, err)
		case ignored:
			nr.State, nr.Reason = StateIgnored, "includeWhen evaluated to false"
		}
		if nr.State != "" {
			res.Nodes = append(res.Nodes, nr)
			continue
		}

		desired, err := node.GetDesired()
		switch {
		case err != nil && kroruntime.IsDataPending(err):
			nr.State, nr.Reason = StatePending, pendingReason("desired state", err)
		case err != nil:
			return nil, fmt.Errorf("node %q: %w", nr.ID, err)
		case len(desired) == 0:
			nr.State, nr.Reason = StateReady, "no desired objects"
		}
		if nr.State != "" {
			res.Nodes = append(res.Nodes, nr)
			continue
		}

		nr.Objects = desired
		switch node.Spec.Meta.Type {
		case krograph.NodeTypeExternal, krograph.NodeTypeExternalCollection:
			nr.State, nr.Reason = observeExternal(node, desired, observed, matched)
		default:
			// Where the controller would GET/LIST live objects, the
			// observed set is synthesised from desired + user overlay.
			items := overlayAll(desired, observed, matched)
			node.SetObserved(items)
			nr.State, nr.Reason = checkReadiness(node)
		}
		res.Nodes = append(res.Nodes, nr)
	}

	// The instance's desired status is computed by the runtime;
	// conditions/state are managed by the controller, not authored here.
	if desired, err := rt.Instance().GetDesired(); err == nil && len(desired) > 0 {
		if status, found, _ := unstructured.NestedMap(desired[0].Object, "status"); found {
			filtered := make(map[string]interface{}, len(status))
			for k, v := range status {
				if k == "conditions" || k == "state" {
					continue
				}
				filtered[k] = v
			}
			_ = unstructured.SetNestedMap(work.Object, filtered, "status")
		}
	}
	// A pending instance status (data not yet resolvable) simply leaves the
	// fields unset — same as a first reconcile on the real controller.

	for _, o := range observed {
		if !matched[o] {
			res.UnmatchedObserved = append(res.UnmatchedObserved, o)
		}
	}
	return res, nil
}

func pendingReason(what string, err error) string {
	return fmt.Sprintf("%s resolution is pending: %s", what, err)
}

// checkReadiness maps a node's CheckReadiness result to a State.
func checkReadiness(node *kroruntime.Node) (State, string) {
	if err := node.CheckReadiness(); err != nil {
		if errors.Is(err, kroruntime.ErrWaitingForReadiness) {
			return StateNotReady, fmt.Sprintf("waiting for readiness: %s", err)
		}
		return StateNotReady, err.Error()
	}
	return StateReady, ""
}

// observeExternal feeds user-supplied objects to an externalRef node: nothing
// is created for external references, so all desired objects must be matched.
func observeExternal(node *kroruntime.Node, desired, observed []*unstructured.Unstructured, matched map[*unstructured.Unstructured]bool) (State, string) {
	id := node.Spec.Meta.ID
	var found []*unstructured.Unstructured
	for _, want := range desired {
		got := matchObserved(want, observed)
		if got == nil {
			return StatePending, fmt.Sprintf("external reference requires an observed object (--observed): %s %s/%s",
				want.GroupVersionKind().String(), want.GetNamespace(), want.GetName())
		}
		matched[got] = true
		found = append(found, got)
	}
	node.SetObserved(found)
	if state, reason := checkReadiness(node); state != StateReady {
		return state, fmt.Sprintf("external reference %q: %s", id, reason)
	}
	return StateReady, ""
}

// matchObserved finds the user-supplied object matching want's identity
// (apiVersion, kind, namespace, name).
func matchObserved(want *unstructured.Unstructured, observed []*unstructured.Unstructured) *unstructured.Unstructured {
	gvk := want.GroupVersionKind()
	for _, o := range observed {
		if o.GroupVersionKind() == gvk &&
			o.GetNamespace() == want.GetNamespace() &&
			o.GetName() == want.GetName() {
			return o
		}
	}
	return nil
}

// overlayAll synthesises observed objects for a node: each desired object
// merged with the matching user-supplied object on top.
func overlayAll(desired, observed []*unstructured.Unstructured, matched map[*unstructured.Unstructured]bool) []*unstructured.Unstructured {
	items := make([]*unstructured.Unstructured, len(desired))
	for i, want := range desired {
		if got := matchObserved(want, observed); got != nil {
			matched[got] = true
			items[i] = mergeObjects(want, got)
		} else {
			items[i] = want.DeepCopy()
		}
	}
	return items
}

// mergeObjects returns overlay merged on top of base: maps merge
// recursively, lists and scalars replace. Neither input is modified.
func mergeObjects(base, overlay *unstructured.Unstructured) *unstructured.Unstructured {
	out := base.DeepCopy()
	out.Object = mergeValue(base.Object, overlay.Object).(map[string]interface{})
	return out
}

func mergeValue(base, overlay interface{}) interface{} {
	bm, bok := base.(map[string]interface{})
	om, ook := overlay.(map[string]interface{})
	if !bok || !ook {
		return overlay
	}
	out := make(map[string]interface{}, len(bm)+len(om))
	for k, v := range bm {
		out[k] = v
	}
	for k, ov := range om {
		if bv, ok := out[k]; ok {
			out[k] = mergeValue(bv, ov)
		} else {
			out[k] = ov
		}
	}
	return out
}
