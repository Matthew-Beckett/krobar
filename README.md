# krobar

krobar is an interim CLI for [kro](https://github.com/kubernetes-sigs/kro)
(Kube Resource Orchestrator). It re-implements the functionality of the
upstream `cmd/kro` CLI so developers and administrators can validate
ResourceGraphDefinitions (RGDs) and generate CRDs, instance manifests, and
resource-graph diagrams from the terminal.

It builds against the published `github.com/kubernetes-sigs/kro` v0.9.3 module.

> **Note:** graph building works **offline by default** — resource schemas are
> resolved from a bundled copy of the Kubernetes OpenAPI document plus any
> CRDs you supply, so `validate` and `generate` need no cluster access. Pass
> `--from-cluster` to resolve schemas via live API discovery instead (as
> upstream does).

## Install

```sh
go install github.com/Matthew-Beckett/krobar/cmd/krobar@latest
```

or build from source:

```sh
make build   # produces ./bin/krobar
```

## Usage

Global flags:

- `--kubeconfig` — path to kubeconfig (default: standard loading rules,
  i.e. `$KUBECONFIG` or `~/.kube/config`, then in-cluster config). Only used
  with `--from-cluster`.
- `--context` — kubeconfig context override. Only used with `--from-cluster`.
- `--verbose` — debug logging to stderr
- `--version` — print version (includes the bundled Kubernetes schema version)

Flags shared by every graph-building command (`validate *`,
`generate *`, `render`, `test`):

- `--crds PATH...` — files or directories of CRDs used for offline schema
  resolution (repeatable; directories are scanned recursively for
  `*.yaml`/`*.yml`/`*.json`, multi-document files supported). RGD documents
  found here are compiled into their generated CRDs — including chains of
  RGDs that reference each other's instance kinds.
- `--from-cluster` — resolve schemas via the live cluster instead of the
  offline resolver (this is when `--kubeconfig`/`--context` take effect).

Commands:

```sh
# Validate an RGD file (use "-" for stdin)
krobar validate rgd -f rgd.yaml
krobar validate rgd rgd.yaml
cat rgd.yaml | krobar validate rgd -f -

# Validate instance manifest(s) against the generated CRD
krobar validate instance -r rgd.yaml -i instance.yaml

# Compare two RGD revisions for API compatibility
krobar validate compat old-rgd.yaml new-rgd.yaml [-o json|yaml]

# Generate the CRD emitted by kro for an RGD (-o yaml|json)
krobar generate crd -f rgd.yaml -o yaml

# Generate a dummy instance manifest of the generated CRD
krobar generate instance -f rgd.yaml --seed 42 -n my-namespace

# Render an interactive HTML graph diagram to stdout
krobar generate diagram -f rgd.yaml > diagram.html

# Canonicalize YAML formatting (2-space indent; canonical key order for RGDs)
krobar fmt rgd.yaml dir/ ...
krobar fmt --check dir/          # print files needing formatting, exit 2
krobar fmt - < rgd.yaml          # read stdin, write stdout

krobar version
krobar completion bash|zsh|fish|powershell
```

The input file may be given via `-f/--file` or as a single positional
argument; `-` reads from standard input.

### Rendering (`krobar render`)

`render` expands an RGD + instance into the concrete resources kro would
create, evaluated by kro's real `pkg/runtime` — CEL templating, `forEach`,
`includeWhen`, `externalRef`, `readyWhen`, and instance status projection all
run through the same engine the controller uses.

```sh
krobar render -f rgd.yaml -i instance.yaml [--observed objects.yaml ...] \
    [--include-instance] [-o yaml|json] [--crds ...] [--from-cluster]
```

- Exactly one instance document is required (`-i -` reads stdin, so
  `krobar generate instance -f rgd.yaml --seed 1 | krobar render -f rgd.yaml -i -`
  composes).
- stdout: rendered objects in dependency (topological) order — a
  `---`-separated YAML stream, or a `v1 List` for `-o json`.
  `--include-instance` appends the defaulted instance with its computed
  `.status`.
- stderr: a `NODE / STATE / DETAIL` summary table and warnings for observed
  objects that matched no node.
- `--observed` files or directories supply live object state (e.g.
  `kubectl get deploy x -o yaml > observed.yaml`, `kind: List` documents are
  unwrapped). Matching is by apiVersion/kind/namespace/name; the observed
  object is deep-merged over the desired object before `readyWhen`
  expressions and status references are evaluated. Nodes whose dependencies
  are unresolved stay `Pending`; `externalRef` nodes are `Pending` until an
  observed object is supplied.
- Exit 0 even with Pending nodes; 1 on errors.

Because rendering is a client-side simulation, the simulated API server is
`desired + observed overlay`: there is no admission, mutating webhook, or
server-side defaulting applied to rendered child resources, and no
controller-observed conditions beyond what `pkg/runtime` computes.

### Fixture tests (`krobar test`)

`krobar test [PATH...]` recursively scans YAML files for documents of kind
`RGDTest` (apiVersion `krobar.dev/v1alpha1`; everything else is ignored) and
executes each as a render-and-assert test:

```yaml
apiVersion: krobar.dev/v1alpha1
kind: RGDTest
metadata:
  name: webapp-defaults
spec:
  rgd: ./webapp-rgd.yaml            # required, relative to the test file
  crds: [./crds]                    # optional, relative; merged with --crds
  instance: {...}                   # inline object — XOR instanceFile
  instanceFile: ./instance.yaml
  observed: [ {k8s object}, ... ]   # optional, same as render --observed
  expect:
    error: "substring"              # build/validate/render must fail like this
    nodes:
      <nodeID>:
        state: Ready                # Ready | NotReady | Pending | Ignored
        count: 1                    # number of rendered objects
        objects:                    # element i subset-matched vs object i
          - spec: {replicas: 3}
    status: {...}                   # subset match on instance .status
```

Expectations are subset matches (expected keys must exist and match
recursively; lists compare element-wise; numbers normalise int/float).
Mismatch messages carry field paths, e.g.
`nodes.deployment.objects[0].spec.replicas: expected 3, got 1`. The spec is
strict-decoded: unknown fields fail loudly. Flags: `--run REGEX` (name
filter), `--junit FILE` (JUnit XML report, one testsuite per file), plus the
graph flags. Output is go-test style; exit 1 on any failure or when no tests
are found.

```sh
krobar test ./rgds --junit report.xml   # CI usage
```

Example fixtures live under `testdata/tests/`.

### Offline schema resolution

krobar compiles in the published Kubernetes OpenAPI v2 document
(`api/openapi-spec/swagger.json` for Kubernetes 1.36 — `krobar version`
reports the bundled version). Built-in kinds resolve from that document;
custom kinds resolve from CRDs supplied via `--crds`. CRD schemas are
augmented with the same `apiVersion`/`kind`/`metadata` (full ObjectMeta)
top-level properties the API server exposes, so offline graph building is
byte-identical to `--from-cluster` output. A `--kubernetes-version` flag
(KREP-021) is intentionally not supported: exactly one schema set is
compiled in.

`validate instance` applies the same checks the API server would:
structural-schema defaulting, pruning of unknown fields (reported as
errors), OpenAPI schema validation, and CEL `x-kubernetes-validations`.

### Exit codes

- `0` — success
- `1` — error or validation failure (invalid RGD/instance, breaking
  compatibility changes)
- `2` — `fmt --check` found files that need formatting

## Intentional differences from upstream `cmd/kro`

- Graph building works offline by default (bundled Kubernetes 1.36 schemas);
  `--from-cluster` restores the upstream online behaviour.
- `--kubeconfig`, `--context`, and `--verbose` flags are actually honoured
  (upstream accepted but ignored them).
- A positional file argument works for `generate` and `validate rgd`; `-`
  reads from stdin.
- CRD/instance YAML marshals via `sigs.k8s.io/yaml`, so `json` struct tags
  are respected (upstream marshalled the CRD with `yaml.v2`, producing wrong
  field names); JSON output is pretty-printed.
- Diagram output is deterministic (nodes/links are emitted in sorted order).
- `generate instance` supports `--seed` for reproducible dummy data and
  `--namespace/-n` for the instance namespace.
- `spec.schema.group`/`scope` default to the API-server values (`kro.run`,
  `Namespaced`) when omitted from the file.
- `generate crd` stamps the CRD's TypeMeta
  (`apiextensions.k8s.io/v1`/`CustomResourceDefinition`), which the upstream
  builder leaves empty, so output can be `kubectl apply`ed directly.
- `generate instance` emits the instance's real `apiVersion`/`kind` from the
  CRD spec (upstream used the CRD object's own TypeMeta, producing empty
  values), and omits `metadata.namespace` for cluster-scoped CRDs.
- `kro.run/cli-version` / `kro.run/version` annotations carry the real krobar
  version instead of the literal `"dev"`, and existing annotations are merged
  rather than replaced.
- The emulator no longer panics on malformed vendor extensions, non-string
  enum values, or inverted min/max constraints.
- Output is written to the command's configured writer (cobra streams), so
  `krobar ... > file` and test capture behave correctly.
- Extra commands: `validate instance`, `validate compat`, `fmt`, `render`,
  `test`.

## Development

- `make build` — build `bin/krobar` with version ldflags
- `make test` — run the test suite with `-race`
- `make lint` — run golangci-lint
- `make snapshot` — local goreleaser snapshot build into `dist/`
- `go vet ./...` — vet; `gofmt -l .` must be empty

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Portions of
this codebase are derived from upstream kro (Copyright The Kubernetes
Authors).
